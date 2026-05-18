package collector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func syncGitRepositoriesWithLogger(
	ctx context.Context,
	config RepoSyncConfig,
	repositoryIDs []string,
	logger *slog.Logger,
) (GitSyncSelection, error) {
	if err := os.MkdirAll(config.ReposDir, 0o755); err != nil {
		return GitSyncSelection{}, fmt.Errorf("create repos dir %q: %w", config.ReposDir, err)
	}
	token, err := resolveGitToken(ctx, config)
	if err != nil && config.SourceMode == "githubOrg" {
		return GitSyncSelection{}, err
	}

	selected := make([]string, 0, len(repositoryIDs))
	for i, repoID := range repositoryIDs {
		if err := ctx.Err(); err != nil {
			return GitSyncSelection{}, err
		}
		checkoutName, err := repoCheckoutName(repoID)
		if err != nil {
			return GitSyncSelection{}, err
		}
		repoPath := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
		event := gitSyncLogEventFor(repoID, i+1, len(repositoryIDs))
		if !hasGitMarker(repoPath) {
			cloned, cloneErr := cloneRepository(ctx, config, repoID, repoPath, token, logger, event)
			if cloneErr == nil && cloned {
				selected = append(selected, repoPath)
			}
			continue
		}
		updated, updateErr := updateRepository(ctx, config, repoPath, token, logger, event)
		if updateErr == nil && updated {
			selected = append(selected, repoPath)
		}
	}
	return GitSyncSelection{
		SelectedRepoPaths: sortUniqueStrings(selected),
	}, nil
}

func cloneRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoID string,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (bool, error) {
	event = event.withOperation("clone")
	logGitSyncStarted(ctx, logger, event)
	remoteURL := repoRemoteURL(config, repoID)
	if remoteURL == "" {
		err := fmt.Errorf("build remote URL for %q", repoID)
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	command := exec.CommandContext(
		ctx,
		"git",
		"clone",
		"--progress",
		fmt.Sprintf("--depth=%d", config.CloneDepth),
		"--single-branch",
		remoteURL,
		repoPath,
	)
	command.Env = gitCommandEnv(config, token)
	var stderr bytes.Buffer
	progress := newGitProgressWriter(ctx, logger, event, &stderr)
	command.Stderr = progress
	if err := command.Run(); err != nil {
		progress.Flush()
		_ = os.RemoveAll(repoPath)
		wrapped := fmt.Errorf("clone %q: %w: %s", repoID, err, sanitizeGitProgressMessage(strings.TrimSpace(stderr.String())))
		logGitSyncFailed(ctx, logger, event, wrapped)
		return false, wrapped
	}
	progress.Flush()
	logGitSyncCompleted(ctx, logger, event, true)
	return true, nil
}

func updateRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (bool, error) {
	event = event.withOperation("fetch")
	branch, err := resolveDefaultBranch(ctx, config, repoPath, token)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	if branch == "" {
		logGitSyncCompleted(ctx, logger, event, false)
		return false, nil
	}

	event.Branch = branch
	logGitSyncStarted(ctx, logger, event)
	if err := gitFetchBranch(ctx, config, repoPath, branch, token, logger, event); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	headSHA, err := gitRevParse(ctx, repoPath, "HEAD", config, token)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	remoteSHA, err := gitRevParse(ctx, repoPath, "refs/remotes/origin/"+branch, config, token)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	if headSHA == remoteSHA {
		logGitSyncCompleted(ctx, logger, event, false)
		return false, nil
	}

	if _, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"checkout",
		"-B",
		branch,
		"refs/remotes/origin/"+branch,
	); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	logGitSyncCompleted(ctx, logger, event, true)
	return true, nil
}

func resolveDefaultBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
) (string, error) {
	output, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"symbolic-ref",
		"refs/remotes/origin/HEAD",
	)
	if err == nil {
		branch := strings.TrimPrefix(strings.TrimSpace(output), "refs/remotes/origin/")
		if branch != "" {
			return branch, nil
		}
	}

	output, err = gitRun(
		ctx,
		repoPath,
		config,
		token,
		"ls-remote",
		"--symref",
		"origin",
		"HEAD",
	)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		branch := strings.TrimPrefix(fields[0], "ref: refs/heads/")
		if branch != "" {
			return branch, nil
		}
	}
	return "", nil
}

func gitFetchBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	branch string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) error {
	_, err := gitRunWithStderrWriter(
		ctx,
		repoPath,
		config,
		token,
		newGitProgressWriter(ctx, logger, event, nil),
		"fetch",
		"--progress",
		"origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch),
		fmt.Sprintf("--depth=%d", config.CloneDepth),
	)
	return err
}

func gitRevParse(
	ctx context.Context,
	repoPath string,
	ref string,
	config RepoSyncConfig,
	token string,
) (string, error) {
	output, err := gitRun(ctx, repoPath, config, token, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitRun(
	ctx context.Context,
	repoPath string,
	config RepoSyncConfig,
	token string,
	args ...string,
) (string, error) {
	return gitRunWithStderrWriter(ctx, repoPath, config, token, nil, args...)
}

func gitRunWithStderrWriter(
	ctx context.Context,
	repoPath string,
	config RepoSyncConfig,
	token string,
	stderrWriter io.Writer,
	args ...string,
) (string, error) {
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "-C", repoPath)
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = gitCommandEnv(config, token)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	if stderrWriter != nil {
		command.Stderr = io.MultiWriter(&stderr, stderrWriter)
	} else {
		command.Stderr = &stderr
	}
	if err := command.Run(); err != nil {
		flushProgressWriter(stderrWriter)
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "),
			err,
			sanitizeGitProgressMessage(strings.TrimSpace(stderr.String())),
		)
	}
	flushProgressWriter(stderrWriter)
	return strings.TrimSpace(stdout.String()), nil
}

func flushProgressWriter(writer io.Writer) {
	flusher, ok := writer.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}

func gitCommandEnv(config RepoSyncConfig, token string) []string {
	env := os.Environ()
	authMethod := strings.ToLower(strings.TrimSpace(config.GitAuthMethod))
	switch authMethod {
	case "token", "githubapp":
		if strings.TrimSpace(token) == "" {
			return env
		}
		index := len(env)
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_COUNT=%d", 1),
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0="+githubHTTPExtraHeader(token),
		)
		_ = index
	case "ssh":
		command := buildSSHCommand(config)
		if command != "" {
			env = append(env, "GIT_SSH_COMMAND="+command)
		}
	}
	return env
}

func buildSSHCommand(config RepoSyncConfig) string {
	privateKeyPath := strings.TrimSpace(config.SSHPrivateKeyPath)
	if privateKeyPath == "" {
		privateKeyPath = "/var/run/secrets/eshu-ssh/id_rsa"
	}
	knownHostsPath := strings.TrimSpace(config.SSHKnownHostsPath)
	if knownHostsPath == "" {
		knownHostsPath = "/var/run/secrets/eshu-ssh/known_hosts"
	}
	strictHosts := "no"
	knownHostsOpt := ""
	if _, err := os.Stat(knownHostsPath); err == nil {
		strictHosts = "yes"
		knownHostsOpt = fmt.Sprintf("-o UserKnownHostsFile=%s", knownHostsPath)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"ssh -i %s %s -o StrictHostKeyChecking=%s",
		privateKeyPath,
		knownHostsOpt,
		strictHosts,
	))
}
