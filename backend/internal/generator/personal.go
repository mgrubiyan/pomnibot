package generator

import "regexp"

// Personal data: notes are a student's own pages, and a password or a phone
// number written between the lines must not become a card, in a set the
// student may share with the group. A fact whose quote holds one is dropped
// with its cards, whatever the model did with it.
var rePersonal = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:парол\p{L}*|password|passwd|пин-?код\p{L}*|pin)(?:[^\p{L}]|$)`),
	regexp.MustCompile(`(?:\+7|(?:^|[^\d])8)[\s(-]*\d{3}[\s)-]*\d{3}[\s-]*\d{2}[\s-]*\d{2}(?:\D|$)`), // phone
	regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\p{L}\w.]{2,}`),                                            // e-mail
	regexp.MustCompile(`(?:^|\D)\d{4}[ -]\d{4}[ -]\d{4}[ -]\d{4}(?:\D|$)`),                           // card number
}

// personalData reports whether s holds a password, a phone number, an e-mail
// or a card number.
func personalData(s string) bool {
	for _, re := range rePersonal {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
