package embedding

// tokenize performs a simple word-piece-like tokenization.
// This is a simplified version — for production, use a proper tokenizer.
func tokenize(text string, maxLen int) []int {
	// [CLS] = 101, [SEP] = 102
	tokens := []int{101} // [CLS]

	words := splitWords(text)
	for _, word := range words {
		if len(tokens) >= maxLen-1 {
			break
		}
		tokenID := hashWord(word)
		tokens = append(tokens, tokenID)
	}

	tokens = append(tokens, 102) // [SEP]
	return tokens
}

func splitWords(text string) []string {
	var words []string
	var current []byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if len(current) > 0 {
				words = append(words, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

func hashWord(word string) int {
	h := 0
	for _, c := range word {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return 1000 + (h % 29000)
}
