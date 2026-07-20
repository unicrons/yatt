package data

// CommonTLDs is a curated, independently-assembled list of widely
// registered TLDs: the classic gTLDs, the short handful of gTLDs commonly
// used as alternative brand extensions, and the ccTLDs of the countries
// with the largest number of internet users. It is one of the inputs to the
// "common" TLD profile (see PROVENANCE.md); the other is AbusedTLDs plus the
// seed's own near-misses, computed in pkg/engine/tldprofile.go.
var CommonTLDs = []string{
	"com", "net", "org", "info", "biz", "name",
	"io", "co", "me", "tv", "cc", "app", "dev", "ai",
	"us", "uk", "ca", "de", "fr", "es", "it", "nl", "se", "no", "dk", "fi",
	"pl", "ru", "ch", "at", "be", "pt", "gr", "cz", "ie", "nz", "au",
	"jp", "cn", "kr", "in", "br", "mx", "ar", "cl", "za", "sg", "hk", "tw",
	"ae", "sa", "il", "tr", "ua", "ro", "hu", "bg", "hr", "si", "sk",
	"lt", "lv", "ee", "is", "lu", "mt", "cy", "li",
}
