package completions

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/cliffren/toc/internal/fileutil"
	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/tui/components/dialog"
)

type filesAndFoldersContextGroup struct {
	prefix string
}

func (cg *filesAndFoldersContextGroup) GetId() string {
	return cg.prefix
}

func (cg *filesAndFoldersContextGroup) GetEntry() dialog.CompletionItemI {
	return dialog.NewCompletionItem(dialog.CompletionItem{
		Title: "Files & Folders",
		Value: "files",
	})
}

// git repo detection, cached per process
var (
	gitRepoOnce sync.Once
	inGitRepo   bool
)

func isInGitRepo() bool {
	gitRepoOnce.Do(func() {
		cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
		cmd.Dir = "."
		inGitRepo = cmd.Run() == nil
	})
	return inGitRepo
}


func processNullTerminatedOutput(outputBytes []byte) []string {
	if len(outputBytes) > 0 && outputBytes[len(outputBytes)-1] == 0 {
		outputBytes = outputBytes[:len(outputBytes)-1]
	}

	if len(outputBytes) == 0 {
		return []string{}
	}

	split := bytes.Split(outputBytes, []byte{0})
	matches := make([]string, 0, len(split))

	for _, p := range split {
		if len(p) == 0 {
			continue
		}

		path := string(p)
		path = filepath.Join(".", path)

		if !fileutil.SkipHidden(path) {
			matches = append(matches, path)
		}
	}

	return matches
}

func (cg *filesAndFoldersContextGroup) getFiles(query string) ([]string, error) {
	if !isInGitRepo() {
		// Browsing mode: empty query or navigated into a dir (ends with "/")
		if query == "" || strings.HasSuffix(query, "/") {
			return cg.getFilesOneLevel(query)
		}
		// Search mode: user typed a filter string → rg max-depth 2
		return cg.getFilesShallow(query)
	}
	return cg.getFilesRg(query)
}

func getShallowRgCmd() *exec.Cmd {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil
	}
	// --hidden: include dotfiles (unlike navigation mode which filters them)
	cmd := exec.Command(rgPath, "--files", "-L", "--null", "--hidden", "--max-depth", "2")
	cmd.Dir = "."
	return cmd
}

// getFilesShallow uses rg with max-depth 2 for non-git dirs when user has typed a query.
// Dotfiles are included (unlike navigation mode) so users can search for them explicitly.
func (cg *filesAndFoldersContextGroup) getFilesShallow(query string) ([]string, error) {
	cmdRg := getShallowRgCmd()
	if cmdRg == nil {
		return cg.getFilesOneLevel(query)
	}

	var rgOut bytes.Buffer
	cmdRg.Stdout = &rgOut
	if err := cmdRg.Run(); err != nil {
		return cg.getFilesOneLevel(query)
	}

	// Parse without SkipHidden — dotfiles are intentionally included here.
	raw := rgOut.Bytes()
	if len(raw) > 0 && raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	var allFiles []string
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) > 0 {
			allFiles = append(allFiles, filepath.Join(".", string(p)))
		}
	}
	return fuzzy.Find(query, allFiles), nil
}

// getFilesOneLevel lists one directory level for non-git directories.
// query may encode a path: "src/co" → list ./src/, filter entries containing "co".
func (cg *filesAndFoldersContextGroup) getFilesOneLevel(query string) ([]string, error) {
	dirPath := "."
	filter := query
	if idx := strings.LastIndex(query, "/"); idx >= 0 {
		sub := query[:idx]
		if sub != "" {
			dirPath = sub
		}
		filter = query[idx+1:]
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		display := name
		if e.IsDir() {
			display = name + "/"
		}
		if filter == "" || strings.Contains(strings.ToLower(display), strings.ToLower(filter)) {
			if dirPath == "." {
				files = append(files, display)
			} else {
				files = append(files, dirPath+"/"+display)
			}
		}
	}
	return files, nil
}

// getFilesRg is the original full-scan implementation used inside git repos.
func (cg *filesAndFoldersContextGroup) getFilesRg(query string) ([]string, error) {
	cmdRg := fileutil.GetRgCmd("") // No glob pattern for this use case
	cmdFzf := fileutil.GetFzfCmd(query)

	var matches []string
	// Case 1: Both rg and fzf available
	if cmdRg != nil && cmdFzf != nil {
		rgPipe, err := cmdRg.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to get rg stdout pipe: %w", err)
		}
		defer rgPipe.Close()

		cmdFzf.Stdin = rgPipe
		var fzfOut bytes.Buffer
		var fzfErr bytes.Buffer
		cmdFzf.Stdout = &fzfOut
		cmdFzf.Stderr = &fzfErr

		if err := cmdFzf.Start(); err != nil {
			return nil, fmt.Errorf("failed to start fzf: %w", err)
		}

		errRg := cmdRg.Run()
		errFzf := cmdFzf.Wait()

		if errRg != nil {
			logging.Warn(fmt.Sprintf("rg command failed during pipe: %v", errRg))
		}

		if errFzf != nil {
			if exitErr, ok := errFzf.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return []string{}, nil // No matches from fzf
			}
			return nil, fmt.Errorf("fzf command failed: %w\nStderr: %s", errFzf, fzfErr.String())
		}

		matches = processNullTerminatedOutput(fzfOut.Bytes())

		// Case 2: Only rg available
	} else if cmdRg != nil {
		logging.Debug("Using Ripgrep with fuzzy match fallback for file completions")
		var rgOut bytes.Buffer
		var rgErr bytes.Buffer
		cmdRg.Stdout = &rgOut
		cmdRg.Stderr = &rgErr

		if err := cmdRg.Run(); err != nil {
			return nil, fmt.Errorf("rg command failed: %w\nStderr: %s", err, rgErr.String())
		}

		allFiles := processNullTerminatedOutput(rgOut.Bytes())
		matches = fuzzy.Find(query, allFiles)

		// Case 3: Only fzf available
	} else if cmdFzf != nil {
		logging.Debug("Using FZF with doublestar fallback for file completions")
		files, _, err := fileutil.GlobWithDoublestar("**/*", ".", 0)
		if err != nil {
			return nil, fmt.Errorf("failed to list files for fzf: %w", err)
		}

		allFiles := make([]string, 0, len(files))
		for _, file := range files {
			if !fileutil.SkipHidden(file) {
				allFiles = append(allFiles, file)
			}
		}

		var fzfIn bytes.Buffer
		for _, file := range allFiles {
			fzfIn.WriteString(file)
			fzfIn.WriteByte(0)
		}

		cmdFzf.Stdin = &fzfIn
		var fzfOut bytes.Buffer
		var fzfErr bytes.Buffer
		cmdFzf.Stdout = &fzfOut
		cmdFzf.Stderr = &fzfErr

		if err := cmdFzf.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return []string{}, nil
			}
			return nil, fmt.Errorf("fzf command failed: %w\nStderr: %s", err, fzfErr.String())
		}

		matches = processNullTerminatedOutput(fzfOut.Bytes())

		// Case 4: Fallback to doublestar with fuzzy match
	} else {
		logging.Debug("Using doublestar with fuzzy match for file completions")
		allFiles, _, err := fileutil.GlobWithDoublestar("**/*", ".", 0)
		if err != nil {
			return nil, fmt.Errorf("failed to glob files: %w", err)
		}

		filteredFiles := make([]string, 0, len(allFiles))
		for _, file := range allFiles {
			if !fileutil.SkipHidden(file) {
				filteredFiles = append(filteredFiles, file)
			}
		}

		matches = fuzzy.Find(query, filteredFiles)
	}

	return matches, nil
}


func (cg *filesAndFoldersContextGroup) GetChildEntries(query string) ([]dialog.CompletionItemI, error) {
	matches, err := cg.getFiles(query)
	if err != nil {
		return nil, err
	}

	items := make([]dialog.CompletionItemI, 0, len(matches))
	for _, file := range matches {
		item := dialog.NewCompletionItem(dialog.CompletionItem{
			Title: file,
			Value: file,
		})
		items = append(items, item)
	}

	return items, nil
}

func NewFileAndFolderContextGroup() dialog.CompletionProvider {
	return &filesAndFoldersContextGroup{
		prefix: "file",
	}
}
