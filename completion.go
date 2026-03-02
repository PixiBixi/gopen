package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// detectShell returns the current shell name from $SHELL, defaulting to "bash".
func detectShell() string {
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "bash", "zsh", "fish":
		return shell
	default:
		return "bash"
	}
}

func printCompletion(shell string) {
	switch shell {
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		fmt.Print(bashCompletion)
	}
}

const bashCompletion = `# gopen bash completion
# Add to ~/.bashrc:
#   eval "$(gopen --completion=bash)"

_gopen() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "${prev}" in
        -r|--remote|-l|--line|--commit|--completion)
            return
            ;;
    esac

    if [[ "${cur}" == -* ]]; then
        COMPREPLY=($(compgen -W "-v --version -c --copy -r --remote -l --line --commit --completion" -- "${cur}"))
    else
        COMPREPLY=($(compgen -f -- "${cur}"))
    fi
}

complete -F _gopen gopen
`

const zshCompletion = `# gopen zsh completion
# Add to ~/.zshrc:
#   eval "$(gopen --completion=zsh)"

_gopen() {
    _arguments \
        '(-v --version)'{-v,--version}'[Print version information]' \
        '(-c --copy)'{-c,--copy}'[Copy URL to clipboard instead of opening browser]' \
        '(-r --remote)'{-r,--remote}'[Git remote to use (default: origin)]:remote name:' \
        '(-l --line)'{-l,--line}'[Highlight line or range (e.g. 42 or 42-50)]:line:' \
        '--commit[Open a specific commit]:hash:' \
        '--completion[Output shell completion script]:shell:(bash zsh fish)' \
        '*:path:_files'
}

compdef _gopen gopen
`

const fishCompletion = `# gopen fish completion
# Add to ~/.config/fish/config.fish:
#   gopen --completion=fish | source

complete -c gopen -s v -l version -d 'Print version information' -f
complete -c gopen -s c -l copy -d 'Copy URL to clipboard instead of opening browser' -f
complete -c gopen -s r -l remote -d 'Git remote to use (default: origin)' -r
complete -c gopen -s l -l line -d 'Highlight line or range (e.g. 42 or 42-50)' -r
complete -c gopen -l commit -d 'Open a specific commit' -r -f
complete -c gopen -l completion -d 'Output shell completion script' -r -f -a 'bash zsh fish'
`
