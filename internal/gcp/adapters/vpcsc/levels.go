package vpcsc

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// LevelResource maps an access level listed under scope. usedBy, when set,
// says how the perimeter or binding it lists under uses it.
func LevelResource(scope, policy string, l Level, usedBy string) model.Resource {
	md := map[string]string{}
	if l.Title != "" && l.Title != l.Name {
		md["title"] = l.Title
	}
	if usedBy != "" {
		md["usedBy"] = usedBy
	}
	var lists []model.List
	if l.Expression != "" {
		md["type"] = "custom"
		lists = []model.List{{ID: "expression", Label: "Expression", Items: FormatCEL(l.Expression)}}
	} else {
		md["type"] = "basic"
		md["conditions"] = strconv.Itoa(len(l.Conditions))
		if len(l.Conditions) > 1 {
			md["combining"] = cmp.Or(l.Combining, "AND")
		}
		if len(l.Conditions) > 0 {
			items := make([]string, 0, len(l.Conditions))
			for _, c := range l.Conditions {
				items = append(items, conditionText(c))
			}
			lists = []model.List{{ID: "conditions", Label: "Conditions", Items: items}}
		}
	}
	return model.Resource{
		ID:          childID(scope, policy, "accessLevels", l.Name),
		Name:        l.Name,
		Service:     model.ServiceAccessLevel,
		ScopeID:     scope,
		Location:    "global",
		Description: l.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "title", "type", "usedBy", "combining", "conditions"),
		Lists:       lists,
	}
}

// conditionText reads one basic-level condition:
// "NOT ip 10.0.0.0/8; regions US, CA; device policy".
func conditionText(c Condition) string {
	var parts []string
	add := func(label string, vs []string) {
		if len(vs) > 0 {
			parts = append(parts, label+" "+strings.Join(vs, ", "))
		}
	}
	add("ip", c.IPSubnetworks)
	add("regions", c.Regions)
	add("members", c.Members)
	add("requires", c.RequiredLevels)
	add("VPC networks", c.VPCNetworks)
	if c.Device != nil {
		parts = append(parts, deviceText(*c.Device))
	}
	s := strings.Join(parts, "; ")
	if s == "" {
		s = "always"
	}
	if c.Negate {
		s = "NOT " + s
	}
	return s
}

// deviceText reads a device policy: "device encrypted, managed BASIC or
// COMPLETE, DESKTOP_MAC ≥ 10.15, screen lock, corp-owned, admin approval".
func deviceText(d Device) string {
	parts := []string{"device"}
	words := func(vs []string) string {
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = strings.ToLower(strings.ReplaceAll(v, "_", " "))
		}
		return strings.Join(out, " or ")
	}
	if len(d.Encryption) > 0 {
		parts = append(parts, words(d.Encryption))
	}
	if len(d.Management) > 0 {
		parts = append(parts, "managed "+strings.Join(d.Management, " or "))
	}
	if len(d.OS) > 0 {
		oses := make([]string, 0, len(d.OS))
		for _, o := range d.OS {
			s := o.Type
			if o.MinVersion != "" {
				s += " ≥ " + o.MinVersion
			}
			if o.Verified {
				s += " verified"
			}
			oses = append(oses, s)
		}
		parts = append(parts, strings.Join(oses, " or "))
	}
	if d.Screenlock {
		parts = append(parts, "screen lock")
	}
	if d.CorpOwned {
		parts = append(parts, "corp-owned")
	}
	if d.AdminApproval {
		parts = append(parts, "admin approval")
	}
	if len(parts) == 1 {
		return "device policy"
	}
	return parts[0] + " " + strings.Join(parts[1:], ", ")
}

// FormatCEL breaks a CEL expression into lines at its && and || operators,
// each operator leading its line, indented by how deep in parentheses it
// sits. Runs of whitespace outside string literals collapse to one space.
// It does not parse CEL; it only reads brackets and quotes.
func FormatCEL(expr string) []string {
	var lines []string
	var cur strings.Builder
	depth, lineDepth := 0, 0
	var quote byte
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			lines = append(lines, strings.Repeat("  ", lineDepth)+t)
		}
		cur.Reset()
	}
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(expr) {
				i++
				cur.WriteByte(expr[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '(' || c == '[' || c == '{':
			depth++
			cur.WriteByte(c)
		case c == ')' || c == ']' || c == '}':
			depth--
			cur.WriteByte(c)
		case (c == '&' || c == '|') && i+1 < len(expr) && expr[i+1] == c:
			flush()
			lineDepth = max(depth, 0)
			cur.WriteString(string([]byte{c, c}) + " ")
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if s := cur.String(); s != "" && !strings.HasSuffix(s, " ") {
				cur.WriteByte(' ')
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return lines
}
