package grok

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cast"
)

const (
	GTypeStr    = "str"
	GTypeString = "string"
	GTypeInt    = "int"
	GTypeFloat  = "float"
	GTypeBool   = "bool"
)

var (
	validPattern    = regexp.MustCompile(`^\w+([-.]\w+)*(:([-.\w]+)(:(string|str|float|int|bool))?)?$`)
	normalPattern   = regexp.MustCompile(`%{([\w-.]+(?::[\w-.]+(?::[\w-.]+)?)?)}`)
	symbolicPattern = regexp.MustCompile(`\W`)
)

var (
	ErrNotCompiled = errors.New("not compiled")
	ErrMismatch    = errors.New("mismatch")
)

// GrokPattern represents a grok pattern with its denormalized regular expression
type GrokPattern struct {
	pattern      string
	denormalized string
	varbType     map[string]string
}

// Pattern returns the original pattern string
func (g *GrokPattern) Pattern() string {
	return g.pattern
}

// Denormalized returns the denormalized regular expression
func (g *GrokPattern) Denormalized() string {
	return g.denormalized
}

// TypedVar returns a copy of the variable type map
func (g *GrokPattern) TypedVar() map[string]string {
	ret := map[string]string{}
	for k, v := range g.varbType {
		ret[k] = v
	}
	return ret
}

// PatternStorageIface defines the interface for pattern storage
type PatternStorageIface interface {
	GetPattern(string) (*GrokPattern, bool)
	SetPattern(string, *GrokPattern)
}

// PatternStorage is a slice-based implementation of PatternStorageIface
type PatternStorage []map[string]*GrokPattern

// GetPattern retrieves a pattern from storage
func (p PatternStorage) GetPattern(pattern string) (*GrokPattern, bool) {
	for _, v := range p {
		if gp, ok := v[pattern]; ok {
			return gp, ok
		}
	}
	return nil, false
}

// SetPattern stores a pattern in the last map of the storage
func (p PatternStorage) SetPattern(patternAlias string, gp *GrokPattern) {
	if len(p) > 0 {
		p[len(p)-1][patternAlias] = gp
	}
}

// DenormalizePattern denormalizes a single pattern to its regular expression
func DenormalizePattern(input string, denormalized ...PatternStorageIface) (*GrokPattern, error) {
	gPattern := &GrokPattern{
		varbType: make(map[string]string),
		pattern:  input,
	}

	pattern := input

	for _, values := range normalPattern.FindAllStringSubmatch(pattern, -1) {
		if !validPattern.MatchString(values[1]) {
			return nil, fmt.Errorf("invalid pattern `%%{%s}`", values[1])
		}

		names := strings.Split(values[1], ":")
		syntax, alias := names[0], names[0]

		// Replace non-word characters with underscore for alias
		if len(names) > 1 {
			alias = symbolicPattern.ReplaceAllString(names[1], "_")
		}

		// Get the data type of the variable, if any
		if len(names) > 2 {
			switch names[2] {
			case GTypeString, GTypeStr:
				gPattern.varbType[alias] = GTypeStr
			case GTypeInt:
				gPattern.varbType[alias] = GTypeInt
			case GTypeFloat:
				gPattern.varbType[alias] = GTypeFloat
			case GTypeBool:
				gPattern.varbType[alias] = GTypeBool
			default:
				return nil, fmt.Errorf("pattern: `%%{%s}`: invalid varb data type: `%s`",
					pattern, names[2])
			}
		}

		if len(denormalized) == 0 {
			return nil, fmt.Errorf("no pattern found for %%{%s}", syntax)
		}

		gP, ok := denormalized[0].GetPattern(syntax)
		if !ok {
			return nil, fmt.Errorf("no pattern found for %%{%s}", syntax)
		}

		// Merge type information from the referenced pattern
		for key, dtype := range gP.varbType {
			if _, ok := gPattern.varbType[key]; !ok {
				gPattern.varbType[key] = dtype
			}
		}

		var buffer bytes.Buffer
		if len(names) > 1 {
			buffer.WriteString("(?P<")
			buffer.WriteString(alias)
			buffer.WriteString(">")
			buffer.WriteString(gP.denormalized)
			buffer.WriteString(")")
		} else {
			buffer.WriteString("(")
			buffer.WriteString(gP.denormalized)
			buffer.WriteString(")")
		}
		pattern = strings.ReplaceAll(pattern, values[0], buffer.String())
	}

	gPattern.denormalized = pattern
	return gPattern, nil
}

// DenormalizePatternsFromMap denormalizes patterns from a map.
// Returns a map of valid denormalized patterns and a map of errors for invalid patterns.
func DenormalizePatternsFromMap(m map[string]string, denormalized ...map[string]*GrokPattern) (map[string]*GrokPattern, map[string]string) {
	patternDeps := map[string]*nodeP{}

	for key, value := range m {
		node := &nodeP{
			cnt:   value,
			cNode: []string{},
		}

		// Find sub-patterns that this pattern depends on
		for _, match := range normalPattern.FindAllStringSubmatch(value, -1) {
			names := strings.Split(match[1], ":")
			syntax := names[0]

			// Check if the dependency exists in the input map
			if _, ok := m[syntax]; ok {
				node.cNode = append(node.cNode, syntax)
			} else {
				// Check if it exists in the denormalized patterns
				found := false
				for _, v := range denormalized {
					if deV, ok := v[syntax]; ok {
						node.cNode = append(node.cNode, syntax)
						patternDeps[syntax] = &nodeP{
							cnt: syntax,
							ptn: deV,
						}
						found = true
						break
					}
				}
				if !found {
					// Dependency will be caught as missing during tree processing
					node.cNode = append(node.cNode, syntax)
				}
			}
		}
		patternDeps[key] = node
	}

	return runTree(patternDeps)
}

// CopyDefalutPatterns returns a copy of the default patterns map
func CopyDefalutPatterns() map[string]string {
	ret := map[string]string{}
	for k, v := range patterns {
		ret[k] = v
	}
	return ret
}

// SubMatchName holds information about named submatches in a regex
type SubMatchName struct {
	name         []string
	subexpIndex  []int
	subexpCount  int
}

// GrokRegexp represents a compiled grok pattern as a regular expression
type GrokRegexp struct {
	grokPattern   *GrokPattern
	re            *regexp.Regexp
	subMatchNames SubMatchName
}

// MatchNames returns the list of named capture group names
func (g *GrokRegexp) MatchNames() []string {
	return g.subMatchNames.name
}

// Run executes the compiled pattern against the content string
// Returns a slice of matched values corresponding to the named groups
func (g *GrokRegexp) Run(content string, trimSpace bool) ([]string, error) {
	if g.re == nil {
		return nil, ErrNotCompiled
	}

	match := g.re.FindStringSubmatchIndex(content)
	if len(match) == 0 {
		return nil, ErrMismatch
	}
	if g.subMatchNames.subexpCount*2 != len(match) {
		return nil, ErrMismatch
	}

	result := make([]string, len(g.subMatchNames.name))

	for i := range g.subMatchNames.name {
		idx := g.subMatchNames.subexpIndex[i]

		left := match[2*idx]
		right := match[2*idx+1]
		if left == -1 || right == -1 {
			continue
		}

		if trimSpace {
			result[i] = strings.TrimSpace(content[left:right])
		} else {
			result[i] = content[left:right]
		}
	}

	return result, nil
}

// GetValByName retrieves a matched value by its capture group name
func (g *GrokRegexp) GetValByName(k string, val []string) (string, bool) {
	if len(val) != len(g.subMatchNames.name) {
		return "", false
	}
	for i, name := range g.subMatchNames.name {
		if name == k {
			return val[i], true
		}
	}
	return "", false
}

// WithTypeInfo returns true if the pattern has type information
func (g *GrokRegexp) WithTypeInfo() bool {
	return len(g.grokPattern.varbType) > 0
}

// RunWithTypeInfo executes the pattern and converts matched values to their typed equivalents
func (g *GrokRegexp) RunWithTypeInfo(content string, trimSpace bool) ([]interface{}, error) {
	ret, err := g.Run(content, trimSpace)
	if err != nil {
		return nil, err
	}

	castDst := make([]interface{}, len(g.subMatchNames.name))

	for i, name := range g.subMatchNames.name {
		castDst[i], _ = g.GetValCastByName(name, ret)
	}

	return castDst, nil
}

// GetValCastByName retrieves a matched value by name and converts it to its typed value
func (g *GrokRegexp) GetValCastByName(k string, val []string) (interface{}, bool) {
	if len(val) != len(g.subMatchNames.name) {
		return nil, false
	}

	for i, name := range g.subMatchNames.name {
		if name == k {
			if varType, ok := g.grokPattern.varbType[name]; ok {
				var dstV interface{}
				switch varType {
				case GTypeInt:
					dstV, _ = cast.ToInt64E(val[i])
				case GTypeFloat:
					dstV, _ = cast.ToFloat64E(val[i])
				case GTypeBool:
					dstV, _ = cast.ToBoolE(val[i])
				case GTypeStr:
					dstV = val[i]
				default:
					return nil, false
				}
				return dstV, true
			} else {
				return val[i], true
			}
		}
	}
	return nil, false
}

// GetValAnyByName retrieves a matched value by name from a slice of any type
func (g *GrokRegexp) GetValAnyByName(k string, val []interface{}) (interface{}, bool) {
	if len(val) != len(g.subMatchNames.name) {
		return "", false
	}
	for i, name := range g.subMatchNames.name {
		if name == k {
			return val[i], true
		}
	}
	return "", false
}

// CompilePattern compiles a grok pattern into a GrokRegexp
func CompilePattern(input string, denormalized PatternStorageIface) (*GrokRegexp, error) {
	gP, err := DenormalizePattern(input, denormalized)
	if err != nil {
		return nil, err
	}
	
	re, err := regexp.Compile(gP.denormalized)
	if err != nil {
		return nil, err
	}

	var subMatchNames SubMatchName
	for i, name := range re.SubexpNames() {
		if name != "" {
			// Update index for duplicate names
			for j := range subMatchNames.name {
				if subMatchNames.name[j] == name {
					subMatchNames.subexpIndex[j] = i
					break
				}
			}

			// Insert name and index
			subMatchNames.name = append(subMatchNames.name, name)
			subMatchNames.subexpIndex = append(subMatchNames.subexpIndex, i)
		}
	}

	subMatchNames.subexpCount = len(re.SubexpNames())

	return &GrokRegexp{
		grokPattern:   gP,
		re:            re,
		subMatchNames: subMatchNames,
	}, nil
}

// CompilePattern2 compiles a pre-denormalized GrokPattern into a GrokRegexp
func CompilePattern2(gP *GrokPattern, denormalized PatternStorageIface) (*GrokRegexp, error) {
	re, err := regexp.Compile(gP.denormalized)
	if err != nil {
		return nil, err
	}

	var subMatchNames SubMatchName
	for i, name := range re.SubexpNames() {
		if name != "" {
			// Update index for duplicate names
			for j := range subMatchNames.name {
				if subMatchNames.name[j] == name {
					subMatchNames.subexpIndex[j] = i
					break
				}
			}

			// Insert name and index
			subMatchNames.name = append(subMatchNames.name, name)
			subMatchNames.subexpIndex = append(subMatchNames.subexpIndex, i)
		}
	}

	subMatchNames.subexpCount = len(re.SubexpNames())

	return &GrokRegexp{
		grokPattern:   gP,
		re:            re,
		subMatchNames: subMatchNames,
	}, nil
}
