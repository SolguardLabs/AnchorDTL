package anchordtl

import "fmt"

type RiskTier string

const (
	RiskTierCore       RiskTier = "core"
	RiskTierStandard   RiskTier = "standard"
	RiskTierRestricted RiskTier = "restricted"
)

type RiskPolicy struct {
	Asset                string `json:"asset"`
	MinGuaranteeRatioBps int64  `json:"min_guarantee_ratio_bps"`
	MaxRouteFeeBps       int64  `json:"max_route_fee_bps"`
	MaxSlashBps          int64  `json:"max_slash_bps"`
	RouteCloseDelay      Epoch  `json:"route_close_delay"`
	ChallengePeriod      Epoch  `json:"challenge_period"`
}

func DefaultPolicy(asset string) RiskPolicy {
	return RiskPolicy{
		Asset:                NormalizeAsset(asset),
		MinGuaranteeRatioBps: 10_000,
		MaxRouteFeeBps:       150,
		MaxSlashBps:          10_000,
		RouteCloseDelay:      2,
		ChallengePeriod:      3,
	}
}

func (p RiskPolicy) Validate() error {
	if p.Asset == "" {
		return fail(CodeInvalid, "policy.validate", "asset is required")
	}
	if p.MinGuaranteeRatioBps <= 0 || p.MinGuaranteeRatioBps > 50_000 {
		return fail(CodeInvalid, "policy.validate", "invalid guarantee ratio")
	}
	if p.MaxRouteFeeBps < 0 || p.MaxRouteFeeBps > 10_000 {
		return fail(CodeInvalid, "policy.validate", "invalid route fee bps")
	}
	if p.MaxSlashBps <= 0 || p.MaxSlashBps > 10_000 {
		return fail(CodeInvalid, "policy.validate", "invalid max slash bps")
	}
	return nil
}

func (p RiskPolicy) RequiredGuarantee(principal Amount) (Amount, error) {
	if err := principal.Validate(); err != nil {
		return Amount{}, err
	}
	if NormalizeAsset(p.Asset) != principal.Asset {
		return Amount{}, fail(CodeAssetMismatch, "policy.required", "principal asset %s not accepted by policy %s", principal.Asset, p.Asset)
	}
	return principal.MulRatio(p.MinGuaranteeRatioBps, 10_000)
}

func (p RiskPolicy) ValidateRoute(spec RouteSpec) error {
	if spec.Asset != NormalizeAsset(p.Asset) {
		return fail(CodeAssetMismatch, "policy.route", "route asset %s not accepted", spec.Asset)
	}
	if spec.FeeBps < 0 || spec.FeeBps > p.MaxRouteFeeBps {
		return fail(CodePolicyRejected, "policy.route", "route fee %s exceeds max %s", FormatBps(spec.FeeBps), FormatBps(p.MaxRouteFeeBps))
	}
	if err := spec.Capacity.Validate(); err != nil {
		return err
	}
	if !spec.Capacity.Positive() {
		return fail(CodePolicyRejected, "policy.route", "route capacity must be positive")
	}
	return nil
}

func (p RiskPolicy) SlashLimit(principal Amount) (Amount, error) {
	return principal.MulRatio(p.MaxSlashBps, 10_000)
}

func (p RiskPolicy) String() string {
	return fmt.Sprintf("asset=%s min=%s fee-max=%s slash-max=%s", p.Asset, FormatBps(p.MinGuaranteeRatioBps), FormatBps(p.MaxRouteFeeBps), FormatBps(p.MaxSlashBps))
}

func TierMultiplier(tier RiskTier) int64 {
	switch tier {
	case RiskTierCore:
		return 80
	case RiskTierRestricted:
		return 130
	default:
		return 100
	}
}
