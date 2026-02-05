package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vjeantet/grok"
)

var (
	grokPatternStorage     grok.PatternStorage
	grokPatternStorageErr  error
)

func getGrokPatternStorage() (grok.PatternStorage, error) {
	// In real usage, this would be wrapped in sync.Once
	if grokPatternStorage == nil && grokPatternStorageErr == nil {
		de, errs := grok.DenormalizePatternsFromMap(grok.CopyDefalutPatterns())
		if len(errs) != 0 {
			errMsgs := make([]string, 0, len(errs))
			for k, v := range errs {
				errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", k, v))
			}
			grokPatternStorageErr = fmt.Errorf("failed to denormalize default patterns: %s", strings.Join(errMsgs, "; "))
			return nil, grokPatternStorageErr
		}
		grokPatternStorage = grok.PatternStorage{de}
	}
	return grokPatternStorage, grokPatternStorageErr
}

// compileGrokToRegex converts a grok pattern to regex and extracts field names
func compileGrokToRegex(grokPatternStr string) (string, []string, error) {
	// Get default patterns (cached via sync.Once in real usage)
	patternStorage, err := getGrokPatternStorage()
	if err != nil {
		return "", nil, err
	}

	// Denormalize the user's grok pattern to regex
	grokPat, err := grok.DenormalizePattern(grokPatternStr, patternStorage)
	if err != nil {
		return "", nil, fmt.Errorf("invalid grok pattern %q: %w", grokPatternStr, err)
	}

	// Get the regex string - uses (?P<name>...) syntax
	regexPattern := grokPat.Denormalized()

	// Compile with Go regex to validate and get field names
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return "", nil, fmt.Errorf("grok pattern %q compiled to invalid regex: %w", grokPatternStr, err)
	}

	return regexPattern, re.SubexpNames(), nil
}

func main() {
	// Example 1: Simple IP pattern
	fmt.Println("=== Example 1: Simple IP Pattern ===")
	regex, fields, err := compileGrokToRegex("%{IP:ip_address}")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Pattern: %%{IP:ip_address}\n")
	fmt.Printf("Fields: %v\n", fields[1:]) // Skip empty first element
	fmt.Printf("Regex: %s...\n\n", regex[:50])

	// Example 2: Apache Common Log
	fmt.Println("=== Example 2: Apache Common Log ===")
	regex, fields, err = compileGrokToRegex("%{COMMONAPACHELOG}")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Pattern: %%{COMMONAPACHELOG}\n")
	fmt.Printf("Fields extracted: %v\n", filterEmptyStrings(fields))
	
	// Test matching
	re := regexp.MustCompile(regex)
	logLine := `127.0.0.1 - - [23/Apr/2014:22:58:32 +0200] "GET /index.php HTTP/1.1" 404 207`
	if re.MatchString(logLine) {
		fmt.Println("✓ Successfully matches Apache log line")
		
		// Extract values
		matches := re.FindStringSubmatch(logLine)
		if len(matches) > 0 {
			fmt.Println("\nExtracted values:")
			for i, name := range fields {
				if name != "" && i < len(matches) {
					fmt.Printf("  %s: %s\n", name, matches[i])
				}
			}
		}
	}
	fmt.Println()

	// Example 3: Custom pattern with type annotations
	fmt.Println("=== Example 3: Pattern with Type Annotations ===")
	regex, fields, err = compileGrokToRegex("%{IP:server_ip} %{NUMBER:port:int} %{WORD:status}")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Pattern: %%{IP:server_ip} %%{NUMBER:port:int} %%{WORD:status}\n")
	fmt.Printf("Fields: %v\n", filterEmptyStrings(fields))
	
	re = regexp.MustCompile(regex)
	testLine := "192.168.1.1 8080 active"
	if re.MatchString(testLine) {
		fmt.Println("✓ Successfully matches test line:", testLine)
		matches := re.FindStringSubmatch(testLine)
		if len(matches) > 0 {
			fmt.Println("\nExtracted values:")
			for i, name := range fields {
				if name != "" && i < len(matches) {
					fmt.Printf("  %s: %s\n", name, matches[i])
				}
			}
		}
	}
}

func filterEmptyStrings(s []string) []string {
	result := []string{}
	for _, str := range s {
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}
