package people

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

var (
	stdinMu     sync.Mutex
	stdinReader *bufio.Reader
)

// PromptPassword 在终端里不回显, 管道输入则读一行.
// 管道上的多次读取共用一个缓冲, 否则第一次会把后续内容一并读走.
func PromptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		raw, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	stdinMu.Lock()
	if stdinReader == nil {
		stdinReader = bufio.NewReader(os.Stdin)
	}
	reader := stdinReader
	stdinMu.Unlock()
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if line == "" && err == io.EOF {
		return "", io.EOF
	}
	return strings.TrimRight(line, "\r\n"), nil
}
