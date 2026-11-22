// Package configpreflight diagnoses external access directives without
// materializing large geo/map blocks. Its findings do not gate policy writes.
// Filesystem access is supplied by the caller.
package configpreflight

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	MaxBytes      = 64 << 20
	MaxFiles      = 256
	MaxDepth      = 16
	MaxStatements = 65536
	maxToken      = 64 << 10
)

type Source interface {
	Open(string) (io.ReadCloser, error)
	Includes(string) ([]string, error)
}

type scanner struct {
	ctx                      context.Context
	source                   Source
	base                     string
	bytes, files, statements int
	active, seen             map[string]bool
	report                   Report
}

func Diagnose(ctx context.Context, source Source, sitePath, globalPath string, main io.Reader) Report {
	s := &scanner{ctx: ctx, source: source, active: map[string]bool{}, seen: map[string]bool{}}
	s.report.Warnings = []Warning{}
	if globalPath != "" {
		s.base = filepath.Dir(globalPath)
	}
	if err := s.scan(sitePath, "site", main, 0); err != nil {
		s.report.Incomplete(err)
		return s.report
	}
	if globalPath != "" {
		if err := s.file(globalPath, "main", 0); err != nil {
			s.report.Incomplete(err)
			return s.report
		}
	}
	s.report.Complete = true
	return s.report
}

func (s *scanner) file(path, scope string, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("include 嵌套超过 16 层")
	}
	path = filepath.Clean(path)
	key := scope + ":" + path
	if s.active[key] {
		return fmt.Errorf("include 存在循环引用: %s", path)
	}
	if s.seen[key] {
		return nil
	}
	if s.files >= MaxFiles {
		return fmt.Errorf("检查文件超过 256 个")
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	r, err := s.source.Open(path)
	if err != nil {
		return &locationError{path: path, err: err}
	}
	defer r.Close()
	s.active[key] = true
	defer delete(s.active, key)
	if err := s.scan(path, scope, r, depth); err != nil {
		return err
	}
	s.seen[key] = true
	return nil
}

// budgetReader accounts for bytes even inside skipped blocks and comments.
type budgetReader struct {
	s *scanner
	r io.Reader
}

func (r budgetReader) Read(p []byte) (int, error) {
	if err := r.s.ctx.Err(); err != nil {
		return 0, err
	}
	remaining := MaxBytes - r.s.bytes
	if len(p) > remaining+1 {
		p = p[:remaining+1]
	}
	n, err := r.r.Read(p)
	r.s.bytes += n
	if r.s.bytes > MaxBytes {
		return 0, fmt.Errorf("检查总内容超过 64 MiB")
	}
	return n, err
}

func (s *scanner) scan(path, scope string, r io.Reader, depth int) error {
	s.files++
	if s.files > MaxFiles {
		return fmt.Errorf("检查文件超过 256 个")
	}
	l := &lexer{r: bufio.NewReaderSize(budgetReader{s, r}, 32<<10), line: 1}
	if err := s.block(l, path, scope, depth, 0, false); err != nil {
		var located *locationError
		if errors.As(err, &located) {
			return err
		}
		return &locationError{path: path, line: l.line, err: err}
	}
	return nil
}

func (s *scanner) block(l *lexer, path, scope string, includeDepth, blockDepth int, nested bool) error {
	if blockDepth > 64 {
		return fmt.Errorf("配置块嵌套超过 64 层")
	}
	var words []string
	wordBytes := 0
	statementLine := 1
	for {
		token, delim, err := l.next()
		if err == io.EOF {
			if nested || len(words) > 0 {
				return fmt.Errorf("配置块或指令未结束")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if delim == 0 {
			if len(words) == 0 {
				statementLine = l.tokenLine
			}
			wordBytes += len(token)
			if wordBytes > maxToken || len(words) >= 4096 {
				return fmt.Errorf("配置指令过长")
			}
			words = append(words, token)
			continue
		}
		if delim == '}' {
			if !nested || len(words) > 0 {
				return fmt.Errorf("意外的配置块结束")
			}
			return nil
		}
		if len(words) == 0 {
			return fmt.Errorf("配置缺少指令名称")
		}
		name := words[0]
		if scope != "skip" {
			s.statements++
			if s.statements > MaxStatements {
				return fmt.Errorf("配置指令总数超过检查上限")
			}
			if scope == "site" || scope == "http" {
				switch name {
				case "auth_basic", "auth_basic_user_file", "auth_request", "allow", "deny", "satisfy", "return":
					message := "该访问指令独立于面板规则生效，可能覆盖或改变统一策略的访问结果。"
					if name == "return" {
						message = "该指令可能提前结束请求，使请求不进入统一策略。"
					}
					s.report.Add(Warning{Path: path, Line: statementLine, Directive: name, Message: message})
				}
			}
			if name == "include" {
				if s.source == nil {
					return fmt.Errorf("无法检查 include: 文件来源未配置")
				}
				if delim != ';' || len(words) != 2 {
					return fmt.Errorf("include 语法无效")
				}
				path := words[1]
				if path == "" || strings.ContainsAny(path, "$\x00\r\n") {
					return fmt.Errorf("无法安全解析动态 include")
				}
				if !filepath.IsAbs(path) {
					if s.base == "" {
						return fmt.Errorf("相对 include 需要明确 nginx.conf 路径")
					}
					path = filepath.Join(s.base, path)
				}
				paths, err := s.source.Includes(filepath.Clean(path))
				if err != nil {
					return err
				}
				if len(paths) > MaxFiles {
					return fmt.Errorf("include 匹配文件超过 256 个")
				}
				for _, p := range paths {
					if err := s.file(p, scope, includeDepth+1); err != nil {
						return err
					}
				}
			}
		}
		words = nil
		wordBytes = 0
		if delim == '{' {
			child := "skip"
			if scope == "site" {
				child = "site"
			} else if scope == "main" && name == "http" {
				child = "http"
			}
			if err := s.block(l, path, child, includeDepth, blockDepth+1, true); err != nil {
				return err
			}
		}
	}
}

type lexer struct {
	r               *bufio.Reader
	line, tokenLine int
	last            byte
}

func (l *lexer) readByte() (byte, error) {
	c, err := l.r.ReadByte()
	if err == nil {
		l.last = c
		if c == '\n' {
			l.line++
		}
	}
	return c, err
}

func (l *lexer) unreadByte() {
	if err := l.r.UnreadByte(); err == nil && l.last == '\n' {
		l.line--
	}
}

func (l *lexer) next() (string, byte, error) {
	for {
		c, err := l.readByte()
		if err != nil {
			return "", 0, err
		}
		if strings.ContainsRune(" \t\r\n", rune(c)) {
			continue
		}
		if c == '#' {
			for {
				c, err = l.readByte()
				if err != nil {
					return "", 0, err
				}
				if c == '\n' {
					break
				}
			}
			continue
		}
		l.tokenLine = l.line
		if c == ';' || c == '{' || c == '}' {
			return "", c, nil
		}
		l.unreadByte()
		break
	}
	var word strings.Builder
	var quote byte
	for {
		c, err := l.readByte()
		if err == io.EOF {
			if quote != 0 {
				return "", 0, fmt.Errorf("配置字符串未结束")
			}
			break
		}
		if err != nil {
			return "", 0, err
		}
		if c == '\\' {
			c, err = l.readByte()
			if err != nil {
				return "", 0, fmt.Errorf("配置转义未结束: %w", err)
			}
			word.WriteByte(c)
		} else if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word.WriteByte(c)
			}
		} else if c == '\'' || c == '"' {
			quote = c
		} else if c == '$' {
			word.WriteByte(c)
			next, _ := l.r.Peek(1)
			if len(next) == 1 && next[0] == '{' {
				for {
					c, err = l.readByte()
					if err != nil {
						return "", 0, fmt.Errorf("变量引用未结束: %w", err)
					}
					word.WriteByte(c)
					if word.Len() > maxToken {
						return "", 0, fmt.Errorf("配置 token 过长")
					}
					if c == '}' {
						break
					}
				}
			}
		} else if strings.ContainsRune(" \t\r\n;{}", rune(c)) {
			l.unreadByte()
			break
		} else {
			word.WriteByte(c)
		}
		if word.Len() > maxToken {
			return "", 0, fmt.Errorf("配置 token 过长")
		}
	}
	if word.Len() == 0 {
		return "\x00empty", 0, nil
	}
	return word.String(), 0, nil
}
