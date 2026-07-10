package anchordtl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type OperatorID string
type RouteID string
type GuaranteeID string
type ObligationID string
type AccountID string
type BatchID string

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{2,96}$`)

func (id OperatorID) String() string   { return string(id) }
func (id RouteID) String() string      { return string(id) }
func (id GuaranteeID) String() string  { return string(id) }
func (id ObligationID) String() string { return string(id) }
func (id AccountID) String() string    { return string(id) }
func (id BatchID) String() string      { return string(id) }

func NewOperatorID(name string) OperatorID {
	return OperatorID(deriveID("op", name))
}

func NewRouteID(operator OperatorID, lane string) RouteID {
	return RouteID(deriveID("route", operator.String(), lane))
}

func NewGuaranteeID(operator OperatorID, asset string, label string) GuaranteeID {
	return GuaranteeID(deriveID("guarantee", operator.String(), NormalizeAsset(asset), label))
}

func NewObligationID(route RouteID, ref string) ObligationID {
	return ObligationID(deriveID("obligation", route.String(), ref))
}

func NewAccountID(owner string, asset string) AccountID {
	return AccountID(deriveID("acct", owner, NormalizeAsset(asset)))
}

func NewBatchID(parts ...string) BatchID {
	return BatchID(deriveID("batch", parts...))
}

func deriveID(prefix string, parts ...string) string {
	items := make([]string, 0, len(parts)+1)
	items = append(items, cleanIDPart(prefix))
	for _, part := range parts {
		items = append(items, cleanIDPart(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(items, "\x1f")))
	return fmt.Sprintf("%s_%s", cleanIDPart(prefix), hex.EncodeToString(sum[:])[:20])
}

func deriveSortedID(prefix string, parts ...string) string {
	cp := append([]string(nil), parts...)
	sort.Strings(cp)
	return deriveID(prefix, cp...)
}

func cleanIDPart(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", "@", "-", "#", "-", "\t", "-")
	s = replacer.Replace(s)
	s = strings.Trim(s, "-_.:")
	if s == "" {
		return "empty"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-_.:")
	if out == "" {
		return "empty"
	}
	if len(out) > 48 {
		return out[:48]
	}
	return out
}

func ValidateID(kind string, value string) error {
	if !idPattern.MatchString(value) {
		return fail(CodeInvalid, "ids.validate", "%s id has invalid format", kind)
	}
	return nil
}

func ShortID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:8] + "..." + value[len(value)-4:]
}

func JoinID(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		clean = append(clean, cleanIDPart(part))
	}
	return strings.Join(clean, ":")
}
