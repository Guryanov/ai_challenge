package chunk

import "strings"

// StructureChunker режет текст по структуре: Markdown-заголовки, а для файлов
// без заголовков — по абзацам. Крупные секции добиваются fixed-нарезкой.
type StructureChunker struct {
	MaxSize int
	MinSize int
	Overlap int
}

// NewStructureChunker создаёт structure-стратегию.
func NewStructureChunker(maxSize, minSize, overlap int) *StructureChunker {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if minSize < 0 || minSize > maxSize {
		minSize = 0
	}
	if overlap < 0 || overlap >= maxSize {
		overlap = 0
	}
	return &StructureChunker{MaxSize: maxSize, MinSize: minSize, Overlap: overlap}
}

// Name возвращает имя стратегии.
func (c *StructureChunker) Name() string { return StrategyStructure }

// rawSection — промежуточное представление секции до нарезки.
type rawSection struct {
	parent string
	path   string
	text   string
	start  int
	end    int
}

type heading struct {
	level int
	title string
}

// Chunk разбивает файл на структурные чанки.
func (c *StructureChunker) Chunk(file File) []Chunk {
	sections, hasHeadings := parseSections(file)
	if !hasHeadings {
		sections = parseParagraphs(file)
	}
	if len(sections) == 0 {
		return nil
	}

	merged := c.mergeSmall(sections)
	fixed := NewFixedChunker(c.MaxSize, c.Overlap)

	var chunks []Chunk
	index := 0
	for _, s := range merged {
		sectionName := s.path
		if sectionName == "" {
			sectionName = file.Name
		}

		runes := []rune(s.text)
		if len(runes) == 0 {
			continue
		}

		if len(runes) > c.MaxSize {
			sub := fixed.split(StrategyStructure, runes, file, sectionName, s.start, index)
			chunks = append(chunks, sub...)
			index += len(sub)
			continue
		}

		chunks = append(chunks, newChunk(StrategyStructure, file, sectionName, index, s.text, s.start, s.end))
		index++
	}

	return chunks
}

// parseSections разбирает Markdown-структуру. Возвращает секции и признак,
// что в файле вообще были заголовки.
func parseSections(file File) ([]rawSection, bool) {
	content := strings.ReplaceAll(file.Content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	var (
		sections    []rawSection
		stack       []heading
		fence       bool
		cur         rawSection
		hasCur      bool
		hasHeadings bool
	)

	flush := func() {
		if !hasCur {
			return
		}
		text := strings.TrimSpace(cur.text)
		if text != "" {
			cur.text = text
			if cur.end < cur.start {
				cur.end = cur.start
			}
			sections = append(sections, cur)
		}
		hasCur = false
	}

	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)

		if isFence(trimmed) {
			fence = !fence
			if !hasCur {
				cur = rawSection{start: lineNo}
				hasCur = true
			}
			cur.text += raw + "\n"
			cur.end = lineNo
			continue
		}

		if !fence {
			if level, title := parseHeading(raw); level > 0 {
				flush()
				for len(stack) > 0 && stack[len(stack)-1].level >= level {
					stack = stack[:len(stack)-1]
				}
				parent := joinHeadings(stack)
				stack = append(stack, heading{level: level, title: title})
				cur = rawSection{
					parent: parent,
					path:   joinHeadings(stack),
					start:  lineNo,
					text:   raw + "\n",
					end:    lineNo,
				}
				hasCur = true
				hasHeadings = true
				continue
			}
		}

		if !hasCur {
			cur = rawSection{start: lineNo}
			hasCur = true
		}
		cur.text += raw + "\n"
		cur.end = lineNo
	}

	flush()
	return sections, hasHeadings
}

// parseParagraphs — fallback для файлов без заголовков: секции по пустым строкам.
func parseParagraphs(file File) []rawSection {
	content := strings.ReplaceAll(file.Content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	var (
		sections []rawSection
		b        strings.Builder
		start    int
		end      int
	)

	flush := func() {
		text := strings.TrimSpace(b.String())
		if text != "" {
			sections = append(sections, rawSection{path: file.Name, text: text, start: start, end: end})
		}
		b.Reset()
		start, end = 0, 0
	}

	for i, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			flush()
			continue
		}
		if b.Len() == 0 {
			start = i + 1
		}
		b.WriteString(raw)
		b.WriteString("\n")
		end = i + 1
	}
	flush()

	return sections
}

// mergeSmall склеивает мелкие соседние секции одного родителя до MinSize.
func (c *StructureChunker) mergeSmall(sections []rawSection) []rawSection {
	if c.MinSize <= 0 || len(sections) < 2 {
		return sections
	}

	result := make([]rawSection, 0, len(sections))
	for _, s := range sections {
		if len(result) > 0 {
			last := &result[len(result)-1]
			combined := last.text + "\n" + s.text
			if len([]rune(last.text)) < c.MinSize && len([]rune(combined)) <= c.MaxSize && last.parent == s.parent {
				last.text = strings.TrimSpace(combined)
				last.end = s.end
				if last.parent != "" {
					last.path = last.parent
				}
				continue
			}
		}
		result = append(result, s)
	}

	return result
}

// isFence сообщает, открывает/закрывает ли строка блок кода.
func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// parseHeading разбирает ATX-заголовок Markdown.
func parseHeading(line string) (int, string) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}

	j := i
	for j < len(line) && line[j] == '#' {
		j++
	}
	level := j - i
	if level == 0 || level > 6 {
		return 0, ""
	}
	if j < len(line) && line[j] != ' ' && line[j] != '\t' {
		return 0, ""
	}

	title := strings.TrimSpace(line[j:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		title = strings.Repeat("#", level)
	}
	return level, title
}

// joinHeadings склеивает стек заголовков в путь «H1 > H2 > H3».
func joinHeadings(stack []heading) string {
	if len(stack) == 0 {
		return ""
	}
	titles := make([]string, len(stack))
	for i, h := range stack {
		titles[i] = h.title
	}
	return strings.Join(titles, " > ")
}
