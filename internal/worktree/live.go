package worktree

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// liveness lists processes whose cwd lies inside a worktree root. The zero
// value means the scan is unsupported on this platform.
type liveness struct {
	supported bool
	// cwds maps the pid of every process outside the caller's own tree (the
	// caller and its ancestors) to its resolved cwd.
	cwds map[int]string
}

// scanProcesses reads every process cwd visible in /proc. Processes it may
// not inspect are skipped. On non-Linux systems it reports unsupported.
func scanProcesses() liveness {
	if runtime.GOOS != "linux" {
		return liveness{}
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return liveness{}
	}
	own := ownTree()
	l := liveness{supported: true, cwds: map[int]string{}}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || own[pid] {
			continue
		}
		cwd, err := os.Readlink("/proc/" + e.Name() + "/cwd")
		if err != nil {
			continue
		}
		l.cwds[pid] = strings.TrimSuffix(cwd, " (deleted)")
	}
	return l
}

// in returns the sorted pids whose cwd is root or below it.
func (l liveness) in(root string) []int {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	var pids []int
	for pid, cwd := range l.cwds {
		if cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator)) {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

// ownTree is the caller's pid and every ancestor's. Hooks run with their cwd
// inside the worktree they act on, so these never count as live users.
func ownTree() map[int]bool {
	own := map[int]bool{os.Getpid(): true}
	for pid := os.Getppid(); pid > 1 && !own[pid]; pid = parentOf(pid) {
		own[pid] = true
	}
	return own
}

// parentOf reads the parent pid from /proc/<pid>/stat, or 0 when unknown.
func parentOf(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// The command name in field 2 may hold spaces and parentheses; fields
	// after the last ')' are fixed: state, then ppid.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}
