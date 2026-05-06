package collector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func syncGitRepositories(
	ctx context.Context,
	config RepoSyncConfig,
	repositoryIDs []string,
) (GitSyncSelection, error) {
	if err := os.MkdirAll(config.ReposDir, 0o755); err != nil {
		return GitSyncSelection{}, fmt.Errorf("create repos dir %q: %w", config.ReposDir, err)
	}
	token, err := resolveGitToken(ctx, config)
	if err != nil && config.SourceMode == "githubOrg" {
		return GitSyncSelection{}, err
	}

	selected := make([]string, 0, len(repositoryIDs))
	for _, repoID := range repositoryIDs {
		if err := ctx.Err(); err != nil {
			return GitSyncSelection{}, err
		}
		checkoutName, err := repoCheckoutName(repoID)
		if err != nil {
			return GitSyncSelection{}, err
		}
		repoPath := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
		if !hasGitMarker(repoPath) {
			cloned, cloneErr := cloneRepository(ctx, config, repoID, repoPath, token)
			if cloneErr == nil && cloned {
				selected = append(selected, repoPath)
			}
			continue
		}
		updated, updateErr := updateRepository(ctx, config, repoPath, token)
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
) (bool, error) {
	remoteURL := repoRemoteURL(config, repoID)
	if remoteURL == "" {
		return false, fmt.Errorf("build remote URL for %q", repoID)
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return false, err
	}
	command := exec.CommandContext(
		ctx,
		"git",
		"clone",
		fmt.Sprintf("--depth=%d", config.CloneDepth),
		"--single-branch",
		remoteURL,
		repoPath,
	)
	command.Env = gitCommandEnv(config, token)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		_ = os.RemoveAll(repoPath)
		return false, fmt.Errorf("clone %q: %w: %s", repoID, err, strings.TrimSpace(stderr.String()))
	}
	return true, nil
}

func updateRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
) (bool, error) {
	branch, err := resolveDefaultBranch(ctx, config, repoPath, token)
	if err != nil {
		return false, err
	}
	if branch == "" {
		return false, nil
	}

	if err := gitFetchBranch(ctx, config, repoPath, branch, token); err != nil {
		return false, err
	}
	headSHA, err := gitRevParse(ctx, repoPath, "HEAD", config, token)
	if err != nil {
		return false, err
	}
	remoteSHA, err := gitRevParse(ctx, repoPath, "refs/remotes/origin/"+branch, config, token)
	if err != nil {
		return false, err
	}
	if headSHA == remoteSHA {
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
		return false, err
	}
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
) error {
	_, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"fetch",
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
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "-C", repoPath)
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = gitCommandEnv(config, token)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
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
