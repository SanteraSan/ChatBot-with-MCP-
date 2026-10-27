package bridge

import (
	"strings"
	"unicode/utf8"
)

const messageLimit = 3500

type Delivery struct {
	Messages []string
	File     string
	Caption  string
}

func PrepareDelivery(text string, late bool) Delivery {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Пустой отчёт."
	}
	if late {
		text = "Отчёт пришёл после напоминания.\n\n" + text
	}
	parts := splitRunes(text, messageLimit)
	if len(parts) <= 3 {
		return Delivery{Messages: parts}
	}
	caption := "Полный отчёт во вложении."
	if late {
		caption = "Отчёт пришёл после напоминания. Полный текст во вложении."
	}
	return Delivery{File: text, Caption: caption}
}

func splitRunes(text string, limit int) []string {
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var chunks []string
	var current strings.Builder
	currentRunes := 0
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimSpace(current.String()))
		current.Reset()
		currentRunes = 0
	}
	for _, paragraph := range strings.Split(text, "\n\n") {
		if utf8.RuneCountInString(paragraph) > limit {
			flush()
			chunks = append(chunks, hardSplit(paragraph, limit)...)
			continue
		}
		extra := utf8.RuneCountInString(paragraph)
		if currentRunes > 0 {
			extra += 2
		}
		if currentRunes > 0 && currentRunes+extra > limit {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
			currentRunes += 2
		}
		current.WriteString(paragraph)
		currentRunes += utf8.RuneCountInString(paragraph)
	}
	flush()
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func hardSplit(text string, limit int) []string {
	runes := []rune(text)
	var parts []string
	for len(runes) > 0 {
		n := limit
		if n > len(runes) {
			n = len(runes)
		}
		parts = append(parts, string(runes[:n]))
		runes = runes[n:]
	}
	return parts
}
