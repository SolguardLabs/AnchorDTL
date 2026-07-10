package anchordtl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Amount struct {
	Asset string `json:"asset"`
	Units int64  `json:"units"`
}

func NewAmount(asset string, units int64) Amount {
	return Amount{Asset: NormalizeAsset(asset), Units: units}
}

func ZeroAmount(asset string) Amount {
	return NewAmount(asset, 0)
}

func NormalizeAsset(asset string) string {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return "UNIT"
	}
	return asset
}

func ParseAmount(asset string, text string) (Amount, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "_", ""))
	if text == "" {
		return Amount{}, fail(CodeInvalid, "amount.parse", "empty amount")
	}
	units, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return Amount{}, wrap(CodeInvalid, "amount.parse", err, "invalid integer amount")
	}
	out := NewAmount(asset, units)
	return out, out.Validate()
}

func (a Amount) Validate() error {
	if strings.TrimSpace(a.Asset) == "" {
		return fail(CodeInvalid, "amount.validate", "asset is required")
	}
	if a.Units < 0 {
		return fail(CodeInvalid, "amount.validate", "negative amount %s", a.String())
	}
	return nil
}

func (a Amount) String() string {
	return fmt.Sprintf("%d %s", a.Units, NormalizeAsset(a.Asset))
}

func (a Amount) IsZero() bool {
	return a.Units == 0
}

func (a Amount) Positive() bool {
	return a.Units > 0
}

func (a Amount) SameAsset(b Amount) bool {
	return NormalizeAsset(a.Asset) == NormalizeAsset(b.Asset)
}

func (a Amount) requireSameAsset(op string, b Amount) error {
	if !a.SameAsset(b) {
		return fail(CodeAssetMismatch, op, "asset mismatch %s != %s", a.Asset, b.Asset)
	}
	return nil
}

func (a Amount) Add(b Amount) (Amount, error) {
	if err := a.requireSameAsset("amount.add", b); err != nil {
		return Amount{}, err
	}
	if b.Units > 0 && a.Units > math.MaxInt64-b.Units {
		return Amount{}, fail(CodeInvalid, "amount.add", "amount overflow")
	}
	if b.Units < 0 && a.Units < math.MinInt64-b.Units {
		return Amount{}, fail(CodeInvalid, "amount.add", "amount underflow")
	}
	return NewAmount(a.Asset, a.Units+b.Units), nil
}

func (a Amount) MustAdd(b Amount) Amount {
	out, err := a.Add(b)
	if err != nil {
		panic(err)
	}
	return out
}

func (a Amount) Sub(b Amount) (Amount, error) {
	if err := a.requireSameAsset("amount.sub", b); err != nil {
		return Amount{}, err
	}
	if a.Units < b.Units {
		return Amount{}, fail(CodeInsufficient, "amount.sub", "%s is below %s", a.String(), b.String())
	}
	return NewAmount(a.Asset, a.Units-b.Units), nil
}

func (a Amount) SubClamp(b Amount) Amount {
	if !a.SameAsset(b) {
		return a
	}
	if a.Units <= b.Units {
		return ZeroAmount(a.Asset)
	}
	return NewAmount(a.Asset, a.Units-b.Units)
}

func (a Amount) Neg() Amount {
	return NewAmount(a.Asset, -a.Units)
}

func (a Amount) LessThan(b Amount) bool {
	return a.SameAsset(b) && a.Units < b.Units
}

func (a Amount) GreaterOrEqual(b Amount) bool {
	return a.SameAsset(b) && a.Units >= b.Units
}

func (a Amount) Min(b Amount) Amount {
	if !a.SameAsset(b) {
		return a
	}
	if a.Units <= b.Units {
		return a
	}
	return b
}

func (a Amount) Max(b Amount) Amount {
	if !a.SameAsset(b) {
		return a
	}
	if a.Units >= b.Units {
		return a
	}
	return b
}

func (a Amount) MulRatio(numerator int64, denominator int64) (Amount, error) {
	if denominator <= 0 {
		return Amount{}, fail(CodeInvalid, "amount.ratio", "denominator must be positive")
	}
	if numerator < 0 {
		return Amount{}, fail(CodeInvalid, "amount.ratio", "numerator must be non-negative")
	}
	if a.Units == 0 || numerator == 0 {
		return ZeroAmount(a.Asset), nil
	}
	if a.Units > math.MaxInt64/numerator {
		return Amount{}, fail(CodeInvalid, "amount.ratio", "amount overflow")
	}
	return NewAmount(a.Asset, (a.Units*numerator)/denominator), nil
}

func (a Amount) Bps(bps int64) (Amount, error) {
	return a.MulRatio(bps, 10_000)
}

func SumAmounts(asset string, amounts ...Amount) (Amount, error) {
	total := ZeroAmount(asset)
	for _, amount := range amounts {
		if err := amount.Validate(); err != nil {
			return Amount{}, err
		}
		next, err := total.Add(amount)
		if err != nil {
			return Amount{}, err
		}
		total = next
	}
	return total, nil
}

func SplitByWeights(total Amount, weights []int64) ([]Amount, error) {
	if err := total.Validate(); err != nil {
		return nil, err
	}
	if len(weights) == 0 {
		return nil, fail(CodeInvalid, "amount.split", "weights are required")
	}
	var weightSum int64
	for _, w := range weights {
		if w < 0 {
			return nil, fail(CodeInvalid, "amount.split", "negative weight")
		}
		if w > math.MaxInt64-weightSum {
			return nil, fail(CodeInvalid, "amount.split", "weight overflow")
		}
		weightSum += w
	}
	if weightSum == 0 {
		return nil, fail(CodeInvalid, "amount.split", "zero total weight")
	}
	out := make([]Amount, len(weights))
	var used int64
	for i, w := range weights {
		units := (total.Units * w) / weightSum
		out[i] = NewAmount(total.Asset, units)
		used += units
	}
	remainder := total.Units - used
	for i := 0; remainder > 0 && i < len(out); i++ {
		if weights[i] == 0 {
			continue
		}
		out[i].Units++
		remainder--
	}
	return out, nil
}

func FormatBps(bps int64) string {
	whole := bps / 100
	frac := bps % 100
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%d.%02d%%", whole, frac)
}
