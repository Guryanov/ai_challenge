package agent

import (
	"regexp"
	"strings"

	"ai-chat/internal/history"
)

// Команды в чате: сообщение, начинающееся с «/», обрабатывается агентом.
// Часть команд отвечает детерминированно (без LLM), часть — синтезирует
// запрос к LLM и выполняется обычным ReAct-циклом с инструментами.

// githubRepoPattern описывает допустимый формат аргумента команды /help: owner/repo.
var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// gitBranchPattern описывает допустимое имя git-ветки (разрешён «/»).
var gitBranchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// prNumberPattern описывает номер pull request.
var prNumberPattern = regexp.MustCompile(`^[0-9]+$`)

const commandsHelpText = "Доступные команды:\n" +
	"- /help <owner/repo> — режим вопросов о GitHub-проекте;\n" +
	"- /branches — список веток проекта;\n" +
	"- /branch <name> — выбрать активную ветку;\n" +
	"- /diff [pr] — получить и проанализировать diff PR (по активной ветке или по номеру);\n" +
	"- /exit — выйти из режима проекта."

const helpUsageText = "Использование: /help <owner/repo>\n" +
	"Пример: /help golang/go\n\n" + commandsHelpText

const branchUsageText = "Использование: /branch <name>\n" +
	"Пример: /branch day30\n\n" + commandsHelpText

const diffUsageText = "Использование: /diff [номер PR]\n" +
	"Без аргумента анализируется PR активной ветки; для этого сначала выберите ветку: /branch <name>.\n\n" +
	commandsHelpText

// noProjectText — подсказка, когда команда требует активный GitHub-проект.
const noProjectText = "Сначала подключите GitHub-проект командой /help <owner/repo>."

// workflowUnavailableText — предупреждение, что инструменты работают только в chat.
const workflowUnavailableText = "Инструменты GitHub MCP доступны только в режиме chat. " +
	"Переключите режим обработки на chat и повторите команду."

// commandOutcome — результат обработки команды: либо готовый детерминированный
// ответ (handled=true), либо синтезированный запрос к LLM (llmMessage != ""),
// который выполняется обычным ReAct-путём с инструментами.
type commandOutcome struct {
	response   AgentResponse
	handled    bool
	llmMessage string
}

// command описывает одну чат-команду.
type command struct {
	name string
	run  func(session *history.Session, args string) commandOutcome
}

// parseCommand разбирает сообщение на имя команды и аргумент.
// ok=false, если сообщение не является командой (не начинается с «/»).
func parseCommand(message string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(message)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}

	rest := strings.TrimPrefix(trimmed, "/")
	if rest == "" {
		return "", "", true
	}

	fields := strings.Fields(rest)
	name = strings.ToLower(fields[0])
	if len(fields) > 1 {
		// Аргумент — первый токен после имени; остаток игнорируем.
		args = fields[1]
	}
	return name, args, true
}

// commands возвращает реестр доступных чат-команд.
func commands() []command {
	return []command{
		{name: "help", run: runHelpCommand},
		{name: "exit", run: runExitCommand},
		{name: "branches", run: runBranchesCommand},
		{name: "branch", run: runBranchCommand},
		{name: "diff", run: runDiffCommand},
	}
}

// handleCommand обрабатывает чат-команду, если сообщение ею является.
func handleCommand(session *history.Session, message string) (commandOutcome, bool) {
	name, args, ok := parseCommand(message)
	if !ok {
		return commandOutcome{}, false
	}

	for _, cmd := range commands() {
		if cmd.name == name {
			return cmd.run(session, args), true
		}
	}

	return commandOutcome{
		response: AgentResponse{Content: "Неизвестная команда: /" + name + "\n\n" + commandsHelpText},
		handled:  true,
	}, true
}

// runHelpCommand включает режим вопросов о GitHub-проекте.
func runHelpCommand(session *history.Session, args string) commandOutcome {
	repo := strings.TrimSpace(args)
	if !githubRepoPattern.MatchString(repo) {
		return fixedOutcome(helpUsageText)
	}

	session.GitHubProject = repo
	// Смена проекта сбрасывает выбранную ветку.
	session.GitBranch = ""

	var b strings.Builder
	b.WriteString("Проект GitHub ")
	b.WriteString(repo)
	b.WriteString(" подключён. Задавайте вопросы о проекте — отвечу по данным репозитория.")
	if session.WorkflowMode == WorkflowModeWorkflow {
		b.WriteString("\n\nВнимание: сейчас включён режим workflow, в котором инструменты не используются. ")
		b.WriteString("Переключитесь в режим chat, чтобы получать ответы по репозиторию.")
	}
	b.WriteString("\n\nВыйти из режима проекта: /exit.")

	return fixedOutcome(b.String())
}

// runExitCommand выключает режим вопросов о GitHub-проекте.
func runExitCommand(session *history.Session, _ string) commandOutcome {
	if session.GitHubProject == "" {
		return fixedOutcome("Режим проекта не активен.")
	}

	repo := session.GitHubProject
	session.GitHubProject = ""
	session.GitBranch = ""
	return fixedOutcome("Режим проекта " + repo + " выключен.")
}

// runBranchesCommand запрашивает у LLM список веток проекта.
func runBranchesCommand(session *history.Session, _ string) commandOutcome {
	if session.GitHubProject == "" {
		return fixedOutcome(noProjectText)
	}
	if session.WorkflowMode == WorkflowModeWorkflow {
		return fixedOutcome(workflowUnavailableText)
	}

	return llmOutcome("Перечисли ветки репозитория " + session.GitHubProject +
		" с помощью инструмента github MCP list_branches и выведи их списком с краткими пояснениями.")
}

// runBranchCommand выбирает активную git-ветку.
func runBranchCommand(session *history.Session, args string) commandOutcome {
	if session.GitHubProject == "" {
		return fixedOutcome(noProjectText)
	}

	name := strings.TrimSpace(args)
	if name == "" {
		return fixedOutcome(branchUsageText)
	}
	if !gitBranchPattern.MatchString(name) {
		return fixedOutcome("Недопустимое имя ветки: " + name + "\n\n" + branchUsageText)
	}

	session.GitBranch = name

	msg := "Активная ветка: " + name + "."
	if session.WorkflowMode == WorkflowModeWorkflow {
		msg += "\n\n" + workflowUnavailableText
	} else {
		msg += " Анализ diff: /diff."
	}
	return fixedOutcome(msg)
}

// runDiffCommand получает и анализирует diff pull request.
func runDiffCommand(session *history.Session, args string) commandOutcome {
	if session.GitHubProject == "" {
		return fixedOutcome(noProjectText)
	}
	if session.WorkflowMode == WorkflowModeWorkflow {
		return fixedOutcome(workflowUnavailableText)
	}

	repo := session.GitHubProject
	arg := strings.TrimSpace(args)

	if arg != "" {
		if !prNumberPattern.MatchString(arg) {
			return fixedOutcome(diffUsageText)
		}
		return llmOutcome("Получи diff pull request #" + arg + " репозитория " + repo +
			" с помощью инструмента github MCP pull_request_read (method=get_diff) и проанализируй изменения: " +
			"что сделано, какие файлы затронуты, какие риски.")
	}

	if session.GitBranch == "" {
		return fixedOutcome("Активная ветка не выбрана.\n" + diffUsageText)
	}

	return llmOutcome("Найди pull request ветки " + session.GitBranch + " (head) репозитория " + repo +
		" с помощью инструментов github MCP (list_pull_requests), получи его diff " +
		"(pull_request_read, method=get_diff) и проанализируй изменения: что сделано, какие файлы затронуты, какие риски. " +
		"Если PR для этой ветки не найден, сообщи об этом и предложи повторить с номером PR: /diff <номер>.")
}

func fixedOutcome(text string) commandOutcome {
	return commandOutcome{response: AgentResponse{Content: text}, handled: true}
}

func llmOutcome(message string) commandOutcome {
	return commandOutcome{llmMessage: message}
}
