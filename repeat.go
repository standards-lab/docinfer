package docinfer

import "github.com/standards-lab/go-ai/protocol"

const (
	reasonRepetition = "repetition"

	repeatMaxPeriod = 1000
	repeatMinCopies = 2
	repeatMinRunes  = 200
)

func trimLoop(h Held) Held {
	if h.Incomplete != string(protocol.ReasonMaxOutputTokens) {
		return h
	}
	if text, ok := trimRepeat(h.Text); ok {
		h.Text, h.Incomplete = text, reasonRepetition
	}
	return h
}

func trimRepeat(text string) (string, bool) {
	r := []rune(text)
	n := len(r)
	for p := 1; p <= repeatMaxPeriod && repeatMinCopies*p <= n; p++ {
		i := n - p - 1
		for i >= 0 && r[i] == r[i+p] {
			i--
		}
		start := i + 1
		if run := n - start; run >= repeatMinCopies*p && run >= repeatMinRunes {
			return string(r[:start+p]), true
		}
	}
	return text, false
}
