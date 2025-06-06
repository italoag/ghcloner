package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Repo struct {
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
}

type model struct {
	repos    []Repo
	current  int
	total    int
	progress progress.Model
	quitting bool
	err      error
	owner    string
	repoType string
	token    string
}

func initialModel(owner, repoType, token string) model {
	return model{
		repos:    []Repo{},
		current:  0,
		total:    0,
		progress: progress.New(progress.WithDefaultGradient()),
		owner:    owner,
		repoType: repoType,
		token:    token,
	}
}

func (m model) Init() tea.Cmd {
	return fetchRepos(m.owner, m.repoType, m.token)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case []Repo:
		m.repos = msg
		m.total = len(msg)
		if m.total == 0 {
			m.err = fmt.Errorf("nenhum repositório encontrado para %s/%s", m.repoType, m.owner)
			m.quitting = true
			return m, tea.Quit
		}
		return m, cloneRepo(m.repos[m.current])

	case cloneFinishedMsg:
		m.current++
		if m.current >= m.total {
			m.quitting = true
			return m, tea.Quit
		}
		percent := float64(m.current) / float64(m.total)
		cmd := m.progress.SetPercent(percent)
		return m, tea.Batch(cmd, cloneRepo(m.repos[m.current]))

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case errorMsg:
		m.err = msg.err
		m.quitting = true
		return m, tea.Quit

	default:
		return m, nil
	}
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\nErro: %v\n\nPressione 'q' para sair\n", m.err)
	}
	if m.quitting {
		if m.total == 0 {
			return "\nNenhum repositório encontrado.\n"
		}
		return fmt.Sprintf("\nClonagem concluída: %d repositórios clonados.\n", m.total)
	}

	// Verificar se há repositórios disponíveis
	if len(m.repos) == 0 {
		return "\nBuscando repositórios...\n"
	}

	// Verificar se o índice atual é válido
	if m.current >= len(m.repos) {
		return "\nTodos os repositórios foram processados.\n"
	}

	bar := m.progress.View()
	info := fmt.Sprintf("Clonando repositório %d de %d: %s\n", m.current+1, m.total, m.repos[m.current].Name)
	return lipgloss.NewStyle().Padding(1, 2).Render(info + bar)
}

type cloneFinishedMsg struct{}
type errorMsg struct{ err error }

func fetchRepos(owner, repoType, token string) tea.Cmd {
	return func() tea.Msg {
		var repos []Repo
		page := 1
		for {
			url := fmt.Sprintf("https://api.github.com/%s/%s/repos?per_page=100&page=%d", repoType, owner, page)

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return errorMsg{fmt.Errorf("erro ao criar requisição: %v", err)}
			}

			// Adicionar headers apropriados
			req.Header.Set("Accept", "application/vnd.github.v3+json")
			req.Header.Set("User-Agent", "ghclone/1.0")

			if token != "" {
				req.Header.Set("Authorization", "token "+token)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return errorMsg{fmt.Errorf("erro ao fazer requisição: %v", err)}
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 401 {
				if token != "" {
					return errorMsg{fmt.Errorf("token inválido ou expirado. Verifique seu GITHUB_TOKEN")}
				} else {
					return errorMsg{fmt.Errorf("autenticação necessária para este usuário/organização")}
				}
			}

			if resp.StatusCode == 404 {
				return errorMsg{fmt.Errorf("usuário ou organização '%s' não encontrado", owner)}
			}

			if resp.StatusCode != http.StatusOK {
				return errorMsg{fmt.Errorf("falha ao buscar repositórios (status %d): %s", resp.StatusCode, string(body))}
			}

			var pageRepos []Repo
			if err := json.NewDecoder(io.NopCloser(strings.NewReader(string(body)))).Decode(&pageRepos); err != nil {
				return errorMsg{fmt.Errorf("erro ao decodificar resposta: %v", err)}
			}

			if len(pageRepos) == 0 {
				break
			}
			repos = append(repos, pageRepos...)
			page++
		}

		return repos
	}
}

func cloneRepo(repo Repo) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "clone", "--depth=1", "--recurse-submodules", repo.CloneURL)
		// Redirecionar output para evitar poluir a interface do Bubbletea
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return errorMsg{err}
		}
		return cloneFinishedMsg{}
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Uso: go run main.go <tipo> <nome> [token]")
		fmt.Println("  <tipo>: 'users' para usuário ou 'orgs' para organização")
		fmt.Println("  <nome>: nome do usuário ou organização")
		fmt.Println("  [token]: (opcional) token de acesso pessoal do GitHub")
		os.Exit(1)
	}
	repoType := os.Args[1]
	owner := os.Args[2]
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" && len(os.Args) >= 4 {
		token = os.Args[3]
	}

	// Token é opcional, mas mostrar aviso se não fornecido
	if token == "" {
		fmt.Println("Aviso: Executando sem token do GitHub. Pode haver limitações de rate limiting.")
	}

	fmt.Printf("Buscando repositórios para %s/%s...\n", repoType, owner)

	p := tea.NewProgram(initialModel(owner, repoType, token))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Erro ao executar o programa: %v\n", err)
		os.Exit(1)
	}
}
