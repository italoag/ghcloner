# ghclone

> 🚀 Um clonador de repositórios GitHub de alta performance e concorrente, construído com Go

[![CI](https://github.com/italoag/ghcloner/workflows/CI/badge.svg)](https://github.com/italoag/ghcloner/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/italoag/ghcloner)](https://goreportcard.com/report/github.com/italoag/ghcloner)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.24.3+-blue.svg)](https://golang.org)

**ghclone** é uma poderosa ferramenta de linha de comando projetada para clonar eficientemente múltiplos repositórios GitHub de forma concorrente. Possui uma interface de terminal aprimorada com rastreamento de progresso em tempo real, logging estruturado e gerenciamento inteligente de pool de workers.

**📖 [English Version](README.md)**

## ✨ Funcionalidades

- **🔄 Processamento Concorrente**: Clone múltiplos repositórios simultaneamente usando pools de workers configuráveis
- **📊 Rastreamento de Progresso em Tempo Real**: Interface terminal interativa com atualizações de progresso ao vivo
- **🎯 Filtragem Inteligente**: Filtragem avançada por linguagem, tamanho, status de fork e data de atualização
- **📁 Organização Flexível**: Suporte para usuários e organizações GitHub
- **🛠️ Múltiplas Interfaces**: Escolha entre CLI e TUI (Interface de Usuário Terminal)
- **📋 Múltiplos Formatos de Saída**: Exporte listas de repositórios como tabela, JSON ou CSV
- **🔐 Suporte a Token**: Integração com token da API GitHub e limitação de taxa
- **🏗️ Design Orientado a Domínio**: Arquitetura limpa com princípios SOLID
- **📝 Logging Estruturado**: Logging abrangente com níveis configuráveis
- **🐳 Suporte Docker**: Imagens Docker prontas para uso

## 🚀 Instalação

### 📦 Binários Pré-compilados

Baixe a versão mais recente da [página de releases](https://github.com/italoag/ghcloner/releases):

```bash
# Linux (amd64)
curl -L https://github.com/italoag/ghcloner/releases/latest/download/ghclone-linux-amd64.tar.gz | tar xz
sudo mv ghclone /usr/local/bin/

# macOS (amd64)
curl -L https://github.com/italoag/ghcloner/releases/latest/download/ghclone-darwin-amd64.tar.gz | tar xz
sudo mv ghclone /usr/local/bin/

# Windows (amd64)
# Baixe o ghclone-windows-amd64.zip e extraia para seu PATH
```

### 🐹 Do Código Fonte (Go)

```bash
# Instalar com Go (requer Go 1.24.3+)
go install github.com/italoag/ghcloner/cmd/ghclone@latest

# Ou clone e compile
git clone https://github.com/italoag/ghcloner.git
cd ghcloner
make build
sudo cp build/ghclone /usr/local/bin/
```

### 🐳 Docker

```bash
# Baixar a imagem
docker pull ghcr.io/italoag/ghclone:latest

# Executar com Docker
docker run --rm -v $(pwd):/workspace ghcr.io/italoag/ghclone:latest clone user octocat

# Criar um alias para conveniência
echo 'alias ghclone="docker run --rm -v $(pwd):/workspace ghcr.io/italoag/ghclone:latest"' >> ~/.bashrc
```

## 📚 Uso

### 🎯 Início Rápido

```bash
# Clonar todos os repositórios de um usuário
ghclone clone user octocat

# Clonar repositórios de organização (pular forks)
ghclone clone org microsoft --skip-forks

# Listar repositórios em formato JSON
ghclone list user torvalds --format json

# Clonar com configurações personalizadas
ghclone clone user kubernetes --concurrency 16 --depth 1 --base-dir ./repos
```

### 🔧 Comando Clone

Clone repositórios de um usuário ou organização GitHub:

```bash
ghclone clone [type] [owner] [flags]
```

**Tipos de Repositório:**
- `user` ou `users` - Clonar de uma conta de usuário GitHub
- `org` ou `orgs` - Clonar de uma organização GitHub

**Exemplos:**

```bash
# Clonagem básica de usuário
ghclone clone user octocat

# Organização com concorrência personalizada
ghclone clone org microsoft --concurrency 8

# Incluir forks e definir diretório personalizado
ghclone clone user torvalds --include-forks --base-dir /tmp/repos

# Clonar branch específica com profundidade rasa
ghclone clone org kubernetes --branch main --depth 5

# Clonar com logging de debug
ghclone clone user facebook --log-level debug
```

**Flags Disponíveis:**

| Flag | Descrição | Padrão |
|------|-----------|---------|
| `--base-dir` | Diretório base para clonagem | `.` |
| `--branch` | Branch específica para clonar | branch padrão |
| `--concurrency` | Número de workers concorrentes | `8` |
| `--depth` | Profundidade do clone (0 para histórico completo) | `1` |
| `--include-forks` | Incluir repositórios fork | `false` |
| `--skip-forks` | Pular repositórios fork | `true` |
| `--token` | Token de acesso pessoal GitHub | `$GITHUB_TOKEN` |
| `--log-level` | Nível de log (debug/info/warn/error) | `info` |

### 📋 Comando List

Liste e filtre repositórios sem clonar:

```bash
ghclone list [type] [owner] [flags]
```

**Exemplos:**

```bash
# Listar repositórios de usuário em formato tabela
ghclone list user octocat

# Exportar repositórios de organização como JSON
ghclone list org microsoft --format json

# Filtrar por linguagem e tamanho
ghclone list user torvalds --language c --min-size 1000000

# Ordenar por tamanho e limitar resultados
ghclone list org kubernetes --sort size --limit 20

# Filtrar por data de atualização
ghclone list user facebook --updated-after 2024-01-01

# Exportar como CSV para planilhas
ghclone list org google --format csv --sort updated
```

**Flags Disponíveis:**

| Flag | Descrição | Padrão |
|------|-----------|---------|
| `--format` | Formato de saída (table/json/csv) | `table` |
| `--sort` | Ordenar por (name/size/updated) | `name` |
| `--limit` | Limitar número de resultados | ilimitado |
| `--min-size` | Tamanho mínimo do repositório (bytes) | `0` |
| `--max-size` | Tamanho máximo do repositório (bytes) | ilimitado |
| `--language` | Filtrar por linguagem de programação | todas |
| `--updated-after` | Filtrar por data de atualização (YYYY-MM-DD) | todas |
| `--include-forks` | Incluir repositórios fork | `false` |
| `--skip-forks` | Pular repositórios fork | `true` |

## ⚙️ Configuração

### 🔑 Autenticação

ghclone suporta tokens de acesso pessoal GitHub para maiores limites de taxa e acesso a repositórios privados:

```bash
# Definir via variável de ambiente
export GITHUB_TOKEN="seu_token_aqui"

# Ou passar diretamente
ghclone clone user octocat --token "seu_token_aqui"
```

**Criando um Token:**
1. Vá para Configurações do GitHub → Configurações de desenvolvedor → Tokens de acesso pessoal
2. Gere um novo token com escopo `repo`
3. Copie o token e defina como variável de ambiente

### 🎨 Funcionalidades da Interface Terminal

Ao clonar repositórios, ghclone fornece uma interface terminal rica:

- **📊 Progresso em Tempo Real**: Atualizações ao vivo do progresso da clonagem
- **⚡ Métricas de Throughput**: Velocidade atual e tempo estimado de conclusão
- **📈 Contadores de Sucesso/Erro**: Rastreie operações bem-sucedidas e falhadas
- **🎯 Operação Atual**: Veja qual repositório está sendo processado
- **📝 Logging Detalhado**: Logs abrangentes com níveis configuráveis

### 🗂️ Estrutura de Diretórios

Por padrão, os repositórios são clonados com esta estrutura:

```
./
├── repo1/
├── repo2/
└── repo3/
```

Você pode personalizar o diretório base:

```bash
# Diretório base personalizado
ghclone clone user octocat --base-dir /home/user/projetos

# Isso cria:
/home/user/projetos/
├── repo1/
├── repo2/
└── repo3/
```

## 🏗️ Arquitetura

ghclone é construído com princípios de arquitetura limpa:

```
cmd/ghclone/           # Ponto de entrada da aplicação
├── main.go

internal/
├── application/       # Camada de lógica de negócio
│   ├── services/      # Serviços da aplicação
│   └── usecases/      # Implementações de casos de uso
├── domain/           # Domínio de negócio principal
│   ├── cloning/      # Domínio de clonagem
│   ├── repository/   # Domínio de repositório
│   └── shared/       # Tipos compartilhados do domínio
├── infrastructure/   # Preocupações externas
│   ├── concurrency/  # Gerenciamento de pool de workers
│   ├── git/          # Operações Git
│   ├── github/       # Cliente da API GitHub
│   └── logging/      # Logging estruturado
└── interfaces/       # Interfaces de usuário
    ├── cli/          # Interface de linha de comando
    └── tui/          # Interface de usuário terminal
```

**Princípios de Design Principais:**
- **Design Orientado a Domínio**: Separação clara da lógica de negócio
- **Princípios SOLID**: Responsabilidade única, inversão de dependência
- **Processamento Concorrente**: Implementação eficiente de pool de workers
- **Tratamento de Erros**: Gerenciamento abrangente de erros
- **Testabilidade**: Interfaces limpas para testes fáceis

## 🛠️ Desenvolvimento

### 📋 Pré-requisitos

- Go 1.24.3 ou posterior
- Git
- Make (opcional, para conveniência)

### 🔨 Compilação

```bash
# Clonar o repositório
git clone https://github.com/italoag/ghcloner.git
cd ghcloner

# Compilar para plataforma atual
make build

# Compilar para todas as plataformas
make build-all

# Compilar binário estático
make build-static
```

### 🧪 Testes

```bash
# Executar todos os testes
make test

# Executar testes com cobertura
make cover

# Executar benchmarks
make bench

# Testes rápidos durante desenvolvimento
make test-fast
```

### 🎯 Verificações de Qualidade

```bash
# Executar linting
make lint

# Formatar código
make fmt

# Executar verificações de segurança
make sec

# Workflow completo de qualidade
make ci
```

### 🐳 Desenvolvimento Docker

```bash
# Compilar imagem Docker
make docker-build

# Executar com Docker
make docker-run

# Enviar para registry
make docker-push
```

## 🤝 Contribuindo

Agradecemos contribuições! Por favor, veja nossas [Diretrizes de Contribuição](CONTRIBUTING.md) para detalhes.

### 📝 Fluxo de Desenvolvimento

1. **Fork** o repositório
2. **Crie** uma branch de funcionalidade: `git checkout -b feature/funcionalidade-incrivel`
3. **Faça** suas alterações seguindo nossos padrões de código
4. **Teste** suas alterações: `make test`
5. **Lint** seu código: `make lint`
6. **Commit** suas alterações: `git commit -m 'Adicionar funcionalidade incrível'`
7. **Push** para a branch: `git push origin feature/funcionalidade-incrivel`
8. **Abra** um Pull Request

### 🐛 Relatórios de Bug

Ao relatar bugs, por favor inclua:
- Sistema operacional e versão
- Versão do Go
- Versão do ghclone (`ghclone --version`)
- Passos para reproduzir
- Comportamento esperado vs real
- Quaisquer logs ou mensagens de erro relevantes

### 💡 Solicitações de Funcionalidades

Adoraríamos ouvir suas ideias! Por favor, abra uma issue com:
- Descrição clara da funcionalidade
- Caso de uso e motivação
- Implementação proposta (se você tiver ideias)

## 📊 Performance

ghclone é otimizado para performance:

- **Processamento Concorrente**: Pools de workers configuráveis (padrão: 8 workers)
- **Eficiência de Memória**: Operações de streaming onde possível
- **Limitação de Taxa**: Respeita limites da API GitHub
- **Clones Rasos**: Profundidade padrão de 1 para clonagem mais rápida
- **Rastreamento de Progresso**: Atualizações em tempo real com sobrecarga mínima

**Benchmarks** (aproximados, variam por rede e sistema):
- **Repositório Único**: 2-5 segundos
- **Organização (50 repos)**: 30-60 segundos com 8 workers
- **Organização Grande (200+ repos)**: 2-5 minutos com 16 workers

## 🔍 Solução de Problemas

### Problemas Comuns

**Erros de Autenticação:**
```bash
# Verificar seu token
curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/user

# Verificar escopos do token
ghclone list user octocat --log-level debug
```

**Limitação de Taxa:**
```bash
# Usar requisições autenticadas
export GITHUB_TOKEN="seu_token_aqui"

# Reduzir concorrência
ghclone clone org org-grande --concurrency 4
```

**Problemas de Rede:**
```bash
# Habilitar logging de debug
ghclone clone user octocat --log-level debug

# Verificar conectividade
curl -I https://api.github.com
```

**Erros de Permissão:**
```bash
# Garantir que o diretório seja gravável
ls -la $(pwd)

# Usar diretório personalizado
ghclone clone user octocat --base-dir /tmp/repos
```

## 📄 Licença

Este projeto está licenciado sob a Licença MIT - veja o arquivo [LICENSE](LICENSE) para detalhes.

## 🙏 Agradecimentos

- [Charm](https://charm.sh/) pelas excelentes bibliotecas TUI
- [Cobra](https://cobra.dev/) pelo framework CLI
- [Zap](https://github.com/uber-go/zap) pelo logging estruturado
- [Ants](https://github.com/panjf2000/ants) pelo gerenciamento de pool de workers

## 📞 Suporte

- 📧 **Issues**: [GitHub Issues](https://github.com/italoag/ghcloner/issues)
- 💬 **Discussões**: [GitHub Discussions](https://github.com/italoag/ghcloner/discussions)
- 📖 **Documentação**: [Wiki](https://github.com/italoag/ghcloner/wiki)

---

Feito com ❤️ por [Italo A. G.](https://github.com/italoag)

**📖 [Leia em Inglês](README.md)**