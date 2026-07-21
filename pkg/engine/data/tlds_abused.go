package data

// AbusedTLDs is a curated list of the new gTLDs and legacy TLDs that
// independent registrar-abuse research (Spamhaus' and Interisle's
// periodically published "most abused TLD" reports, among others) has
// repeatedly named as disproportionately represented in phishing and
// typosquatting takedown data — generally because they are cheap, or were
// once free through a registrar promotion. It is one of the inputs to the
// "common" TLD profile (see PROVENANCE.md).
var AbusedTLDs = []string{
	"xyz", "top", "club", "online", "site", "live", "click", "link", "work",
	"icu", "buzz", "monster", "cyou", "rest", "quest", "sbs", "cfd",
	"pw", "tk", "ml", "ga", "cf", "gq",
	"su", "vip", "win", "bid", "loan", "download", "review", "party",
	"stream", "trade", "accountant", "science", "men", "faith", "date",
	"gdn", "kim", "email", "webcam",
}
