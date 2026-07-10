package anchordtl

import "sort"

type SlashReason string

const (
	SlashReasonTimeout      SlashReason = "timeout"
	SlashReasonIntegrity    SlashReason = "integrity"
	SlashReasonCapacity     SlashReason = "capacity"
	SlashReasonReconcileGap SlashReason = "reconcile_gap"
)

type SlashRequest struct {
	GuaranteeID   GuaranteeID    `json:"guarantee_id"`
	ObligationIDs []ObligationID `json:"obligation_ids"`
	Amount        Amount         `json:"amount"`
	Reason        SlashReason    `json:"reason"`
	Epoch         Epoch          `json:"epoch"`
	Memo          string         `json:"memo"`
}

type SlashAllocation struct {
	ObligationID ObligationID `json:"obligation_id"`
	RouteID      RouteID      `json:"route_id"`
	Amount       Amount       `json:"amount"`
}

type SlashReceipt struct {
	GuaranteeID GuaranteeID        `json:"guarantee_id"`
	BatchID     BatchID            `json:"batch_id"`
	Reason      SlashReason        `json:"reason"`
	Epoch       Epoch              `json:"epoch"`
	Total       Amount             `json:"total"`
	Allocations []SlashAllocation  `json:"allocations"`
	RouteTotals map[RouteID]Amount `json:"route_totals"`
}

type SlashingService struct {
	Policy      RiskPolicy
	Obligations *ObligationBook
	Guarantees  *GuaranteeStore
	Routes      *RouteBook
	Sink        EventSink
}

func NewSlashingService(policy RiskPolicy, obligations *ObligationBook, guarantees *GuaranteeStore, routes *RouteBook, sink EventSink) *SlashingService {
	return &SlashingService{
		Policy:      policy,
		Obligations: obligations,
		Guarantees:  guarantees,
		Routes:      routes,
		Sink:        sink,
	}
}

func (s *SlashingService) Slash(request SlashRequest) (SlashReceipt, error) {
	if err := request.Validate(); err != nil {
		return SlashReceipt{}, err
	}
	guarantee, err := s.Guarantees.Get(request.GuaranteeID)
	if err != nil {
		return SlashReceipt{}, err
	}
	obligations, err := s.Obligations.OpenIDs(request.ObligationIDs)
	if err != nil {
		return SlashReceipt{}, err
	}
	if len(obligations) == 0 {
		return SlashReceipt{}, fail(CodeInvalid, "slash.apply", "no obligations selected")
	}
	if request.Amount.Asset != guarantee.Asset {
		return SlashReceipt{}, fail(CodeAssetMismatch, "slash.apply", "slash asset does not match guarantee")
	}
	totalOutstanding := ZeroAmount(request.Amount.Asset)
	weights := make([]int64, 0, len(obligations))
	for _, obligation := range obligations {
		if obligation.GuaranteeID != request.GuaranteeID {
			return SlashReceipt{}, fail(CodeConflict, "slash.apply", "obligation %s belongs to a different guarantee", obligation.ID)
		}
		outstanding := obligation.Outstanding()
		next, err := totalOutstanding.Add(outstanding)
		if err != nil {
			return SlashReceipt{}, err
		}
		totalOutstanding = next
		weights = append(weights, outstanding.Units)
	}
	if totalOutstanding.LessThan(request.Amount) {
		return SlashReceipt{}, fail(CodeInsufficient, "slash.apply", "slash exceeds outstanding exposure")
	}
	limit, err := s.Policy.SlashLimit(totalOutstanding)
	if err != nil {
		return SlashReceipt{}, err
	}
	if limit.LessThan(request.Amount) {
		return SlashReceipt{}, fail(CodePolicyRejected, "slash.apply", "slash exceeds configured cap")
	}
	parts, err := SplitByWeights(request.Amount, weights)
	if err != nil {
		return SlashReceipt{}, err
	}
	allocations := make(map[ObligationID]Amount, len(obligations))
	receiptAllocations := make([]SlashAllocation, 0, len(obligations))
	routeTotals := make(map[RouteID]Amount)
	for i, obligation := range obligations {
		part := parts[i]
		if part.IsZero() {
			continue
		}
		allocations[obligation.ID] = part
		receiptAllocations = append(receiptAllocations, SlashAllocation{
			ObligationID: obligation.ID,
			RouteID:      obligation.RouteID,
			Amount:       part,
		})
		current, ok := routeTotals[obligation.RouteID]
		if !ok {
			current = ZeroAmount(part.Asset)
		}
		next, err := current.Add(part)
		if err != nil {
			return SlashReceipt{}, err
		}
		routeTotals[obligation.RouteID] = next
	}
	sort.Slice(receiptAllocations, func(i, j int) bool {
		return receiptAllocations[i].ObligationID < receiptAllocations[j].ObligationID
	})
	primaryRoute := obligations[0].RouteID
	total, err := guarantee.ApplyPenaltyBatch(allocations, primaryRoute, request.Epoch)
	if err != nil {
		return SlashReceipt{}, err
	}
	for _, allocation := range receiptAllocations {
		obligation, err := s.Obligations.Get(allocation.ObligationID)
		if err != nil {
			return SlashReceipt{}, err
		}
		if err := obligation.MarkPenalty(allocation.Amount, request.Epoch); err != nil {
			return SlashReceipt{}, err
		}
		if s.Routes != nil {
			if route, err := s.Routes.Get(allocation.RouteID); err == nil {
				_ = route.Metrics.AddPenalty(allocation.Amount)
			}
		}
	}
	receipt := SlashReceipt{
		GuaranteeID: request.GuaranteeID,
		BatchID:     NewBatchID(request.GuaranteeID.String(), request.Memo, request.EpochString()),
		Reason:      request.Reason,
		Epoch:       request.Epoch,
		Total:       total,
		Allocations: receiptAllocations,
		RouteTotals: routeTotals,
	}
	if s.Sink != nil {
		event := NewEvent(EventSlashRecorded, request.Epoch, request.GuaranteeID.String()).
			With("amount", total.String()).
			With("reason", string(request.Reason)).
			With("batch", receipt.BatchID.String())
		s.Sink.Record(event)
	}
	return receipt, nil
}

func (r SlashRequest) Validate() error {
	if r.GuaranteeID == "" {
		return fail(CodeInvalid, "slash.validate", "guarantee id is required")
	}
	if len(r.ObligationIDs) == 0 {
		return fail(CodeInvalid, "slash.validate", "obligation ids are required")
	}
	if err := r.Amount.Validate(); err != nil {
		return err
	}
	if !r.Amount.Positive() {
		return fail(CodeInvalid, "slash.validate", "slash amount must be positive")
	}
	if r.Reason == "" {
		return fail(CodeInvalid, "slash.validate", "reason is required")
	}
	seen := make(map[ObligationID]struct{}, len(r.ObligationIDs))
	for _, id := range r.ObligationIDs {
		if id == "" {
			return fail(CodeInvalid, "slash.validate", "empty obligation id")
		}
		if _, ok := seen[id]; ok {
			return fail(CodeInvalid, "slash.validate", "duplicate obligation id %s", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (r SlashRequest) EpochString() string {
	return JoinID(r.GuaranteeID.String(), r.Amount.String(), string(r.Reason))
}
