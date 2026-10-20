package desktop

import (
	"errors"
	"strings"
)

// StartFailure names the part of startup that failed, so the UI can explain
// what the user can do about it instead of echoing a wrapped library error.
type StartFailure int

const (
	// FailOther is a failure with no specific advice (the wrapped error is
	// the whole story).
	FailOther StartFailure = iota
	// FailDatabase means the SQLite file could not be opened: corrupt,
	// unreadable, or owned by someone else.
	FailDatabase
	// FailCacheDir means the image cache directory could not be created or
	// opened: a missing parent, or no permission.
	FailCacheDir
)

// StartError is a startup failure with the path and the kind of problem, so
// the first-run and settings screens can say:
//
//	数据库无法打开：/home/me/.config/manga-reader/manga-reader.db
//	文件可能已损坏，或没有读写权限。可先备份后删除该文件，应用会新建一个。
//
//	（原始错误：unable to open database file: ...）
type StartError struct {
	Kind StartFailure
	Path string
	Err  error
}

func (e *StartError) Error() string { return e.Explain() }

func (e *StartError) Unwrap() error { return e.Err }

// Explain is the multi-line message for the UI: what failed, the path, what
// to try, then the library's own words.
func (e *StartError) Explain() string {
	advice := ""
	switch e.Kind {
	case FailDatabase:
		advice = "文件可能已损坏，或没有读写权限。可先备份后删除该文件，应用会在下次启动时新建一个；" +
			"也可以在配置文件中把数据库路径指向别处。"
	case FailCacheDir:
		advice = "目录无法创建或写入。请检查该路径是否存在、是否有权限，或把缓存目录改到用户目录下。"
	default:
		advice = ""
	}
	lines := []string{}
	if e.Kind == FailDatabase {
		lines = append(lines, "数据库无法打开："+e.Path)
	} else if e.Kind == FailCacheDir {
		lines = append(lines, "图片缓存目录无法打开："+e.Path)
	} else if e.Path != "" {
		lines = append(lines, e.Path)
	}
	if advice != "" {
		lines = append(lines, advice)
	}
	if e.Err != nil {
		lines = append(lines, "（原始错误："+e.Err.Error()+"）")
	}
	return strings.Join(lines, "\n")
}

// AsStartError pulls a *StartError out of err, or nil.
func AsStartError(err error) *StartError {
	var se *StartError
	if errors.As(err, &se) {
		return se
	}
	return nil
}
