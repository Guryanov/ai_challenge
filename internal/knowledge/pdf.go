package knowledge

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ledongthuc/pdf"
)

// isPDF определяет PDF по расширению или сигнатуре файла.
func isPDF(name string, data []byte) bool {
	if strings.EqualFold(path.Ext(name), ".pdf") {
		return true
	}
	return hasPDFHeader(data)
}

func hasPDFHeader(data []byte) bool {
	return bytes.HasPrefix(data, []byte("%PDF-"))
}

// extractPDFText извлекает текстовый слой из PDF.
// Поддерживаются PDF, содержащие текст (не сканы).
func extractPDFText(data []byte) (text string, err error) {
	if !hasPDFHeader(data) {
		return "", fmt.Errorf("файл не является PDF")
	}

	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("не удалось разобрать PDF: %v", r)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("не удалось открыть PDF: %w", err)
	}
	if reader.NumPage() == 0 {
		return "", fmt.Errorf("в PDF нет страниц")
	}

	plain, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("не удалось извлечь текст из PDF: %w", err)
	}
	raw, err := io.ReadAll(plain)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать текст из PDF: %w", err)
	}

	text = normalizePDFText(string(raw))
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("в PDF не найден текстовый слой (возможно, это скан)")
	}
	return text, nil
}

// normalizePDFText убирает лишние пустые строки, сохраняя разбиение по абзацам.
func normalizePDFText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var b strings.Builder
	b.Grow(len(text))
	blank := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}
