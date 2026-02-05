package grok

import (
	"regexp"
	"testing"
)

func TestCopyDefalutPatterns(t *testing.T) {
	patterns := CopyDefalutPatterns()
	
	if len(patterns) == 0 {
		t.Fatal("Expected non-empty patterns map")
	}
	
	// Check for some essential patterns
	essentialPatterns := []string{"USERNAME", "IP", "COMMONAPACHELOG", "NUMBER"}
	for _, key := range essentialPatterns {
		if _, ok := patterns[key]; !ok {
			t.Errorf("Expected pattern %s to exist", key)
		}
	}
	
	// Verify it's a copy (modifying shouldn't affect original)
	originalLen := len(patterns)
	patterns["TEST_PATTERN"] = "test"
	newPatterns := CopyDefalutPatterns()
	if len(newPatterns) != originalLen {
		t.Error("CopyDefalutPatterns should return a copy, not a reference")
	}
}

func TestDenormalizePatternsFromMap(t *testing.T) {
	tests := []struct {
		name          string
		patterns      map[string]string
		wantValid     []string
		wantInvalid   []string
	}{
		{
			name: "simple pattern",
			patterns: map[string]string{
				"SIMPLE": `\d+`,
			},
			wantValid:   []string{"SIMPLE"},
			wantInvalid: []string{},
		},
		{
			name: "pattern with dependency",
			patterns: map[string]string{
				"BASE":    `\d+`,
				"DERIVED": `%{BASE}`,
			},
			wantValid:   []string{"BASE", "DERIVED"},
			wantInvalid: []string{},
		},
		{
			name: "pattern with default dependency",
			patterns: map[string]string{
				"MYPATTERN": `%{NUMBER}`,
			},
			wantValid:   []string{"MYPATTERN"},
			wantInvalid: []string{},
		},
		{
			name: "pattern with missing dependency",
			patterns: map[string]string{
				"BROKEN": `%{NONEXISTENT}`,
			},
			wantValid:   []string{},
			wantInvalid: []string{"BROKEN"},
		},
		{
			name: "circular dependency",
			patterns: map[string]string{
				"A": `%{B}`,
				"B": `%{A}`,
			},
			wantValid:   []string{},
			wantInvalid: []string{"A", "B"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultPatterns := CopyDefalutPatterns()
			denormalized, _ := DenormalizePatternsFromMap(defaultPatterns)
			
			valid, invalid := DenormalizePatternsFromMap(tt.patterns, denormalized)
			
			// Check valid patterns
			for _, key := range tt.wantValid {
				if _, ok := valid[key]; !ok {
					t.Errorf("Expected pattern %s to be valid", key)
				}
			}
			
			// Check invalid patterns
			for _, key := range tt.wantInvalid {
				if _, ok := invalid[key]; !ok {
					t.Errorf("Expected pattern %s to be invalid", key)
				}
			}
			
			// Check no unexpected valid patterns
			if len(valid) != len(tt.wantValid) {
				t.Errorf("Expected %d valid patterns, got %d", len(tt.wantValid), len(valid))
			}
			
			// Check no unexpected invalid patterns
			if len(invalid) != len(tt.wantInvalid) {
				t.Errorf("Expected %d invalid patterns, got %d", len(tt.wantInvalid), len(invalid))
			}
		})
	}
}

func TestDenormalizePattern(t *testing.T) {
	// First denormalize default patterns
	defaultPatterns := CopyDefalutPatterns()
	denormalized, errs := DenormalizePatternsFromMap(defaultPatterns)
	
	if len(errs) != 0 {
		t.Fatalf("Failed to denormalize default patterns: %v", errs)
	}
	
	storage := PatternStorage{denormalized}
	
	tests := []struct {
		name        string
		pattern     string
		wantError   bool
		testText    string
		shouldMatch bool
	}{
		{
			name:        "simple IP pattern",
			pattern:     "%{IP:ip}",
			wantError:   false,
			testText:    "192.168.1.1",
			shouldMatch: true,
		},
		{
			name:        "username pattern",
			pattern:     "%{USERNAME:user}",
			wantError:   false,
			testText:    "john_doe",
			shouldMatch: true,
		},
		{
			name:        "complex apache log pattern",
			pattern:     "%{COMMONAPACHELOG}",
			wantError:   false,
			testText:    `127.0.0.1 - - [23/Apr/2014:22:58:32 +0200] "GET /index.php HTTP/1.1" 404 207`,
			shouldMatch: true,
		},
		{
			name:        "pattern with type annotation",
			pattern:     "%{NUMBER:port:int}",
			wantError:   false,
			testText:    "8080",
			shouldMatch: true,
		},
		{
			name:      "invalid pattern syntax",
			pattern:   "%{INVALID SYNTAX}",
			wantError: true,
		},
		{
			name:      "nonexistent pattern",
			pattern:   "%{DOESNOTEXIST}",
			wantError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gp, err := DenormalizePattern(tt.pattern, storage)
			
			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			
			if gp.Pattern() != tt.pattern {
				t.Errorf("Pattern() = %q, want %q", gp.Pattern(), tt.pattern)
			}
			
			if gp.Denormalized() == "" {
				t.Error("Denormalized() returned empty string")
			}
			
			// Test if the denormalized pattern is valid regex
			re, err := regexp.Compile(gp.Denormalized())
			if err != nil {
				t.Fatalf("Denormalized pattern is not valid regex: %v", err)
			}
			
			// If we have test text, verify matching behavior
			if tt.testText != "" {
				matched := re.MatchString(tt.testText)
				if matched != tt.shouldMatch {
					t.Errorf("Pattern match = %v, want %v for text %q", matched, tt.shouldMatch, tt.testText)
				}
			}
		})
	}
}

func TestGrokPatternMethods(t *testing.T) {
	defaultPatterns := CopyDefalutPatterns()
	denormalized, _ := DenormalizePatternsFromMap(defaultPatterns)
	storage := PatternStorage{denormalized}
	
	pattern := "%{NUMBER:port:int}"
	gp, err := DenormalizePattern(pattern, storage)
	if err != nil {
		t.Fatalf("Failed to denormalize pattern: %v", err)
	}
	
	// Test Pattern()
	if gp.Pattern() != pattern {
		t.Errorf("Pattern() = %q, want %q", gp.Pattern(), pattern)
	}
	
	// Test Denormalized()
	if gp.Denormalized() == "" {
		t.Error("Denormalized() returned empty string")
	}
	
	// Test TypedVar()
	tv := gp.TypedVar()
	if tv["port"] != "int" {
		t.Errorf("TypedVar()[\"port\"] = %q, want \"int\"", tv["port"])
	}
	
	// Verify TypedVar returns a copy
	tv["port"] = "modified"
	tv2 := gp.TypedVar()
	if tv2["port"] != "int" {
		t.Error("TypedVar() should return a copy, not a reference")
	}
}

func TestPatternStorage(t *testing.T) {
	gp1 := &GrokPattern{
		pattern:      "test1",
		denormalized: `\d+`,
		varbType:     map[string]string{},
	}
	
	gp2 := &GrokPattern{
		pattern:      "test2",
		denormalized: `[a-z]+`,
		varbType:     map[string]string{},
	}
	
	storage := PatternStorage{
		map[string]*GrokPattern{
			"PATTERN1": gp1,
		},
		map[string]*GrokPattern{
			"PATTERN2": gp2,
		},
	}
	
	// Test GetPattern
	retrieved, ok := storage.GetPattern("PATTERN1")
	if !ok {
		t.Error("Expected to find PATTERN1")
	}
	if retrieved != gp1 {
		t.Error("Retrieved pattern doesn't match original")
	}
	
	retrieved, ok = storage.GetPattern("PATTERN2")
	if !ok {
		t.Error("Expected to find PATTERN2")
	}
	if retrieved != gp2 {
		t.Error("Retrieved pattern doesn't match original")
	}
	
	// Test GetPattern for non-existent pattern
	_, ok = storage.GetPattern("NONEXISTENT")
	if ok {
		t.Error("Expected not to find NONEXISTENT pattern")
	}
	
	// Test SetPattern
	gp3 := &GrokPattern{
		pattern:      "test3",
		denormalized: `\w+`,
		varbType:     map[string]string{},
	}
	storage.SetPattern("PATTERN3", gp3)
	
	retrieved, ok = storage.GetPattern("PATTERN3")
	if !ok {
		t.Error("Expected to find PATTERN3 after SetPattern")
	}
	if retrieved != gp3 {
		t.Error("Retrieved pattern doesn't match set pattern")
	}
}

func TestClientUsagePattern(t *testing.T) {
	// This test simulates the client's usage pattern
	de, errs := DenormalizePatternsFromMap(CopyDefalutPatterns())
	if len(errs) != 0 {
		t.Fatalf("Failed to denormalize default patterns: %v", errs)
	}
	
	patternStorage := PatternStorage{de}
	
	// Test pattern compilation
	grokPatternStr := "%{COMMONAPACHELOG}"
	grokPat, err := DenormalizePattern(grokPatternStr, patternStorage)
	if err != nil {
		t.Fatalf("Invalid grok pattern %q: %v", grokPatternStr, err)
	}
	
	// Get the regex string
	regexPattern := grokPat.Denormalized()
	
	// Compile with Go regex to validate
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		t.Fatalf("Grok pattern %q compiled to invalid regex: %v", grokPatternStr, err)
	}
	
	// Test with actual log line
	logLine := `127.0.0.1 - - [23/Apr/2014:22:58:32 +0200] "GET /index.php HTTP/1.1" 404 207`
	if !re.MatchString(logLine) {
		t.Error("Regex should match the log line")
	}
	
	// Verify we can extract field names
	subexpNames := re.SubexpNames()
	if len(subexpNames) == 0 {
		t.Error("Expected to find named capture groups")
	}
	
	// Verify some expected fields
	hasClientIP := false
	hasResponse := false
	for _, name := range subexpNames {
		if name == "clientip" {
			hasClientIP = true
		}
		if name == "response" {
			hasResponse = true
		}
	}
	
	if !hasClientIP {
		t.Error("Expected to find 'clientip' field")
	}
	if !hasResponse {
		t.Error("Expected to find 'response' field")
	}
}

func TestTypeAnnotations(t *testing.T) {
	defaultPatterns := CopyDefalutPatterns()
	denormalized, _ := DenormalizePatternsFromMap(defaultPatterns)
	storage := PatternStorage{denormalized}
	
	tests := []struct {
		name         string
		pattern      string
		expectedType string
		fieldName    string
	}{
		{
			name:         "int type",
			pattern:      "%{NUMBER:count:int}",
			expectedType: "int",
			fieldName:    "count",
		},
		{
			name:         "float type",
			pattern:      "%{NUMBER:ratio:float}",
			expectedType: "float",
			fieldName:    "ratio",
		},
		{
			name:         "string type",
			pattern:      "%{WORD:name:string}",
			expectedType: "str",
			fieldName:    "name",
		},
		{
			name:         "bool type",
			pattern:      "%{WORD:enabled:bool}",
			expectedType: "bool",
			fieldName:    "enabled",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gp, err := DenormalizePattern(tt.pattern, storage)
			if err != nil {
				t.Fatalf("Failed to denormalize pattern: %v", err)
			}
			
			tv := gp.TypedVar()
			if tv[tt.fieldName] != tt.expectedType {
				t.Errorf("TypedVar()[%q] = %q, want %q", tt.fieldName, tv[tt.fieldName], tt.expectedType)
			}
		})
	}
}
