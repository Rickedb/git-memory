package gm

import (
	"os"
	"path/filepath"
)

// There are no config files and no environment variables. Like git, which is "the .git folder", git-memory is "the
// ~/.git-memory folder": that is where everything lives. Behaviour is fixed by these defaults.
type Config struct {
	Briefing          BriefingConfig
	Recall            RecallConfig
	Capture           CaptureConfig
	DebounceCommitSec int // commit session pages at most this often on Stop (SessionEnd/PreCompact always commit)
	MaxAutoPerPrompt  int // memories saved from a single prompt ("remember that …")
}

type BriefingConfig struct{ MaxChars, RecentSessions, RecentMemories, MaxDecisions int }

type RecallConfig struct {
	Enabled                         bool
	MaxChars, Limit, MinPromptChars int
}

type CaptureConfig struct {
	// Exclude are glob patterns (repo-relative) for files that are never recorded.
	Exclude                         []string
	MaxPromptChars, MaxOutcomeChars int
}

func defaultConfig() Config {
	return Config{
		Briefing:          BriefingConfig{MaxChars: 6000, RecentSessions: 3, RecentMemories: 8, MaxDecisions: 30},
		Recall:            RecallConfig{Enabled: true, MaxChars: 2500, Limit: 4, MinPromptChars: 15},
		Capture:           CaptureConfig{Exclude: []string{"**/.env", "**/.env.*", "**/*.pem", "**/*.key", "**/secrets/**"}, MaxPromptChars: 1500, MaxOutcomeChars: 1500},
		DebounceCommitSec: 300,
		MaxAutoPerPrompt:  5,
	}
}

// memoryDir is the memory repository: the .git-memory folder in the user's home. It is a plain git repository that every
// git-memory command runs git in.
func memoryDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		fail("cannot find your home directory, where the memory repository (.git-memory) lives")
	}
	return filepath.Join(home, ".git-memory")
}
