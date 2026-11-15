package tui

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Suggesting a category from payees that look like the one being categorized. Banks write the
// same shop in many ways ("REWE SAGT DANKE 4711", "REWE Markt Berlin"), so the exact payee
// has often not been seen, while another spelling of it has. A payee is cut into its words
// (letters only: numbers and dates change from one booking to the next), and two payees look
// alike when most of what the shorter one says is in the other. A word that many payees share
// ("Deutsche", "Berlin") says little and counts for less than one few payees have ("Bahn"),
// so that "Deutsche Bahn" does not borrow the category of "Deutsche Bank".

// similarMin is how much of the shorter payee has to be in the other for them to look alike.
const similarMin = 0.6

// payeeNoise are words that are no part of who is paid: legal forms and what a bank writes
// before the name of the shop.
var payeeNoise = map[string]bool{
	"gmbh": true, "mbh": true, "ltd": true, "inc": true, "llc": true, "sarl": true, "ohg": true,
	"the": true, "und": true, "and": true,
	"card": true, "payment": true, "purchase": true, "visa": true, "sepa": true, "pos": true,
	"kartenzahlung": true, "lastschrift": true, "zahlung": true, "sagt": true, "danke": true,
	"pago": true, "compra": true, "tarjeta": true,
}

// payeeWords are the words of a payee that say who it is, lower case, each once.
func payeeWords(s string) []string {
	var words []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if utf8.RuneCountInString(w) < 3 || payeeNoise[w] || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
	}
	return words
}

// wordCounts says in how many payees each word appears.
func wordCounts(payees []payeeStat) map[string]int {
	df := make(map[string]int)
	for _, p := range payees {
		for _, w := range p.words {
			df[w]++
		}
	}
	return df
}

// weigh is how much a word says: more the fewer payees have it.
func (m *Model) weigh(word string) float64 {
	return math.Log(1 + float64(len(m.payees)+1)/float64(max(m.payeeDF[word], 1)))
}

// likeness is how much of the shorter of two payees (as words) the other one says, from 0
// to 1.
func (m *Model) likeness(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inB := make(map[string]bool, len(b))
	for _, w := range b {
		inB[w] = true
	}
	var shared, wa, wb float64
	for _, w := range a {
		wa += m.weigh(w)
		if inB[w] {
			shared += m.weigh(w)
		}
	}
	for _, w := range b {
		wb += m.weigh(w)
	}
	return shared / math.Min(wa, wb)
}

// similarCategory is the category of the payees that look like payee, of the kind that
// matches the money (income or expense): the one with most bookings among them (each counted
// as much as it looks alike) and, between equals, the one used most recently. It is empty
// when none looks alike enough. A payee spelt the same way is not "like" it: that is the
// category of the same payee, which the picker offers before this one.
func (m *Model) similarCategory(payee string, isIncome bool) string {
	words := payeeWords(payee)
	if len(words) == 0 {
		return ""
	}
	votes := map[string]float64{}
	var order []string // the categories voted for, the one of the most recently used payee first
	for _, p := range m.payees {
		if p.category == "" || strings.HasPrefix(p.category, "Income:") != isIncome || strings.EqualFold(p.name, payee) {
			continue
		}
		if l := m.likeness(words, p.words); l >= similarMin {
			if _, ok := votes[p.category]; !ok {
				order = append(order, p.category)
			}
			votes[p.category] += l * float64(p.count)
		}
	}
	best, found := "", false
	for _, c := range order {
		if !found || votes[c] > votes[best] { // on a tie the earlier stays
			best, found = c, true
		}
	}
	return best
}
