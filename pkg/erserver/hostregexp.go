package erserver

import (
	"regexp"
	"strings"
)

var hostnameRegexpRe = regexp.MustCompile(`\{[^}]+\}`)

// "hellohttp.{[^.]+}.fn61.net" => "^hellohttp.[^.]+.fn61.net$"
func hostnameRegexpSyntaxToRegexp(in string) (*regexp.Regexp, error) {
	// escape regexp-related chars that could appear in a hostname (like ".")
	escaped := escapeRegexChars(in)

	// seek for regexp sections in strings like "foo.{regexp}.example.com"
	// we will now match the "{regexp}" sections
	regexPlaceholdersUnescaped := hostnameRegexpRe.ReplaceAllStringFunc(escaped, func(match string) string {
		// "{foobar}" => "foobar"
		enclosingCharsRemoved := match[1 : len(match)-1]

		// now since match is a regex, but the outer escaping ruined our regexp, re-fix it
		return unescapeRegexChars(enclosingCharsRemoved)
	})

	anchored := "^" + regexPlaceholdersUnescaped + "$"

	return regexp.Compile(anchored)
}

func escapeRegexChars(in string) string {
	return strings.Replace(in, ".", `\.`, -1)
}

func unescapeRegexChars(in string) string {
	return strings.Replace(in, `\.`, `.`, -1)
}
