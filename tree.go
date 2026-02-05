package grok

import (
	"fmt"
)

// nodeP represents a pattern node in the dependency graph
type nodeP struct {
	cnt   string        // content: the pattern string
	ptn   *GrokPattern  // pre-denormalized pattern (if available)
	cNode []string      // child nodes: dependencies
}

// path tracks the current path through the dependency graph for cycle detection
type path struct {
	m map[string]struct{} // set of nodes in current path
	l []string            // ordered list of nodes in current path
}

// runTree processes the pattern dependency graph and returns denormalized patterns
// Returns a map of successfully denormalized patterns and a map of errors
func runTree(m map[string]*nodeP) (map[string]*GrokPattern, map[string]string) {
	ret := map[string]*GrokPattern{}
	invalid := map[string]string{}
	pt := &path{
		m: map[string]struct{}{},
		l: []string{},
	}
	
	for name, v := range m {
		if err := dfs(ret, m, name, v, pt); err != nil {
			invalid[name] = err.Error()
		}
	}
	
	return ret, invalid
}

// dfs performs depth-first search to resolve pattern dependencies
func dfs(deP map[string]*GrokPattern, top map[string]*nodeP, startName string, start *nodeP, pt *path) error {
	// Check for circular dependency
	if _, ok := pt.m[startName]; ok {
		lineStr := ""
		for _, k := range pt.l {
			lineStr += k + " -> "
		}
		lineStr += startName
		return fmt.Errorf("circular dependency: pattern %s", lineStr)
	}

	// Add current node to path
	pt.m[startName] = struct{}{}
	pt.l = append(pt.l, startName)
	defer func() {
		delete(pt.m, startName)
		pt.l = pt.l[:len(pt.l)-1]
	}()

	// If already denormalized, return early
	if _, ok := deP[startName]; ok {
		return nil
	}

	// If this is a leaf node (no dependencies) or has a pre-denormalized pattern
	if len(start.cNode) == 0 {
		if start.ptn != nil {
			// Use the pre-denormalized pattern
			deP[startName] = start.ptn
			return nil
		}
		// Try to denormalize with what we have
		if ptn, err := DenormalizePattern(start.cnt, PatternStorage{deP}); err != nil {
			return err
		} else {
			deP[startName] = ptn
		}
		return nil
	}

	// Process all dependencies first
	for _, name := range start.cNode {
		cNode, ok := top[name]
		if !ok || cNode == nil {
			return fmt.Errorf("no pattern found for %%{%s}", name)
		}

		// Recursively denormalize the dependency
		if err := dfs(deP, top, name, cNode, pt); err != nil {
			return err
		}
	}

	// Now denormalize this pattern with all dependencies available
	if ptn, err := DenormalizePattern(start.cnt, PatternStorage{deP}); err != nil {
		return err
	} else {
		deP[startName] = ptn
	}

	return nil
}
