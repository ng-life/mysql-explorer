package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
)

var commands = []string{"serve", "connections", "schemas", "tables", "describe", "query", "completion"}

func commandFlags(command string) []string {
	out := []string{"--config", "--help"}
	switch command {
	case "serve":
		return append(out, "--listen")
	case "connections":
		return append(out, "--format")
	case "schemas":
		return append(out, "--connection", "--format")
	case "tables":
		return append(out, "--connection", "--schema", "--format")
	case "describe":
		return append(out, "--connection", "--schema", "--table", "--indexes", "--ddl", "--format")
	case "query":
		return append(out, "--connection", "--schema", "--sql", "--file", "--limit", "--offset", "--mode", "--format")
	}
	return out
}

type completionContext struct {
	command   string
	values    map[string]string
	valueFlag string
	prefix    string
	equals    string
}

// completionWords accepts the current incomplete word as its last argument.
// It deliberately does not use the strict execution parser or execute SQL.
func completionWords(args []string) completionContext {
	c := completionContext{values: map[string]string{}}
	if len(args) == 0 {
		return c
	}
	c.prefix = args[len(args)-1]
	for _, a := range args[:len(args)-1] {
		if c.valueFlag != "" {
			c.values[c.valueFlag] = a
			c.valueFlag = ""
			continue
		}
		if strings.HasPrefix(a, "-") {
			name, value, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if inline {
				c.values[name] = value
				continue
			}
			if name != "help" && name != "h" && name != "indexes" && name != "ddl" {
				c.valueFlag = name
			}
		} else if c.command == "" {
			c.command = a
		}
	}
	if strings.HasPrefix(c.prefix, "-") {
		if flag, value, ok := strings.Cut(c.prefix, "="); ok {
			c.valueFlag = strings.TrimLeft(flag, "-")
			c.prefix = value
			c.equals = flag + "="
		}
	}
	return c
}

// complete is a quiet line protocol for shells. Network failure yields no
// candidates, never raw errors; metadata reads have a short independent deadline.
func complete(ctx context.Context, args []string, stdout io.Writer) int {
	c := completionWords(args)
	candidates := []string{}
	switch c.valueFlag {
	case "mode":
		if c.command == "query" {
			candidates = []string{"query", "explain", "explain_analyze"}
		}
	case "format":
		candidates = []string{"table", "json"}
	case "connection", "schema", "table":
		path := c.values["config"]
		if path == "" {
			path = config.DefaultPath()
		}
		cfg, err := config.Read(path)
		if err != nil {
			return 0
		}
		if c.valueFlag == "connection" {
			for _, cl := range cfg.Clusters {
				candidates = append(candidates, cl.Name)
			}
			break
		}
		id, err := selectConnection(cfg, c.values["connection"])
		if err != nil {
			return 0
		}
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		pool := mysql.NewPool(cfg.Clusters)
		defer pool.Close()
		reader, err := pool.Get(bounded, id)
		if err != nil {
			return 0
		}
		if c.valueFlag == "schema" {
			candidates, err = reader.Databases(bounded)
		} else {
			schema := c.values["schema"]
			if schema == "" {
				return 0
			}
			candidates, err = reader.Tables(bounded, schema)
		}
		if err != nil {
			return 0
		}
	case "":
		if c.command == "" {
			candidates = append(candidates, commands...)
			candidates = append(candidates, "--config", "--help")
		} else if c.command == "completion" {
			candidates = []string{"zsh", "bash"}
		} else {
			candidates = commandFlags(c.command)
		}
	}
	sort.Strings(candidates)
	previous := ""
	for _, candidate := range candidates {
		if candidate == "" || candidate == previous || !strings.HasPrefix(candidate, c.prefix) {
			continue
		}
		previous = candidate
		// Newlines and terminal controls cannot be represented safely in this protocol.
		if strings.IndexFunc(candidate, unicode.IsControl) >= 0 {
			continue
		}
		if _, err := fmt.Fprintln(stdout, c.equals+candidate); err != nil {
			return 1
		}
	}
	return 0
}

func completionScript(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "用法：mysql-explorer completion <zsh|bash>")
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "请指定补全 shell：zsh 或 bash")
		return 2
	}
	var script string
	switch args[0] {
	case "zsh":
		script = zshCompletion
	case "bash":
		script = bashCompletion
	default:
		fmt.Fprintln(stderr, "仅支持 zsh 和 bash")
		return 2
	}
	if _, err := io.WriteString(stdout, script); err != nil {
		fmt.Fprintln(stderr, "无法写入补全脚本")
		return 1
	}
	return 0
}

const zshCompletion = `# mysql-explorer zsh completion; load after compinit.
_mysql_explorer() {
  local previous="${words[CURRENT-1]}" current="${words[CURRENT]}"
  if [[ "$previous" == --config || "$previous" == -config || "$previous" == --file || "$previous" == -file ]]; then
    _files
    return
  fi
  if [[ "$current" == --config=* || "$current" == --file=* ]]; then
    local option="${current%%=*}="
    compset -P "${option}"
    _files
    return
  fi
  local -a candidates
  candidates=("${(@f)$("${words[1]}" __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}")
  (( ${#candidates} )) && compadd -a candidates
}
compdef _mysql_explorer mysql-explorer
`

const bashCompletion = `# mysql-explorer bash completion (also supports macOS Bash 3).
_mysql_explorer() {
  local current="${COMP_WORDS[COMP_CWORD]}" previous="${COMP_WORDS[COMP_CWORD-1]}" candidate word option="" i last
  local -a args
  args=()
  COMPREPLY=()
  # Bash may split --flag=value at '=' because it is a readline word break.
  for ((i=1; i<=COMP_CWORD; i++)); do
    word="${COMP_WORDS[i]}"
    last=$((${#args[@]}-1))
    if (( last >= 0 )) && [[ "$word" == "=" && "${args[last]}" == -* ]]; then
      args[last]="${args[last]}="
    elif (( last >= 0 )) && [[ "${args[last]}" == -*= ]]; then
      args[last]="${args[last]}$word"
    else
      args+=("$word")
    fi
  done
  last=$((${#args[@]}-1))
  if (( last >= 0 )); then current="${args[last]}"; fi
  if [[ "$current" == --*= || "$current" == --*=* ]]; then option="${current%%=*}="; fi
  if [[ "$previous" == --config || "$previous" == -config || "$previous" == --file || "$previous" == -file || "$option" == --config= || "$option" == --file= ]]; then
    while IFS= read -r candidate; do
      if [[ -n "$option" && "${COMP_WORDS[COMP_CWORD]}" == --* ]]; then candidate="$option$candidate"; fi
      COMPREPLY+=("$candidate")
    done < <(compgen -f -- "${current#*=}")
    type compopt >/dev/null 2>&1 && compopt -o filenames
    return
  fi
  while IFS= read -r candidate; do
    if [[ -n "$option" && "${COMP_WORDS[COMP_CWORD]}" != --* ]]; then candidate="${candidate#*=}"; fi
    [[ -n "$candidate" ]] && COMPREPLY+=("$candidate")
  done < <("${COMP_WORDS[0]}" __complete "${args[@]}" 2>/dev/null)
}
complete -F _mysql_explorer mysql-explorer
`
