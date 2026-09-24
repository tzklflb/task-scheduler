package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// runApplyCandidate は、targetとcandidateのSHA-256ハッシュを再検証してから、
// candidateの内容を一時ファイル経由でtargetへ反映する。再検証から書き込みまでは
// 原子的ではないため、その間の競合は防げない。
func runApplyCandidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("apply-candidate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	targetArg := flags.String("target", "", "target schedule YAML path")
	candidateArg := flags.String("candidate", "", "candidate schedule YAML path")
	expected := flags.String("expected-sha256", "", "current target SHA-256")
	absent := flags.Bool("expect-absent", false, "require an absent target")
	allowAsOfChange := flags.Bool("allow-as-of-change", false, "allow settings.as_of to change")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if *targetArg == "" || *candidateArg == "" {
		fmt.Fprintln(stderr, "--target and --candidate are required")
		return 2
	}
	if (*expected == "") == !*absent {
		fmt.Fprintln(stderr, "exactly one of --expected-sha256 or --expect-absent is required")
		return 2
	}
	if *expected != "" && !sha256Pattern.MatchString(*expected) {
		fmt.Fprintln(stderr, "--expected-sha256 must be a lowercase SHA-256 hexadecimal value")
		return 2
	}
	if *absent && *allowAsOfChange {
		fmt.Fprintln(stderr, "--allow-as-of-change cannot be used with --expect-absent")
		return 2
	}

	epicRoot, epicID, target, candidate, err := applyPaths(*targetArg, *candidateArg)
	if err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 2
	}
	defer epicRoot.Close()
	candidateBytes, candidateDigest, err := readRegularFile(epicRoot, candidate)
	if err != nil {
		fmt.Fprintf(stderr, "NG: 候補ファイルを読み込めません: %v\n", err)
		return 1
	}

	var (
		targetInfo  os.FileInfo
		targetBytes []byte
	)
	if *absent {
		err = requireAbsent(epicRoot, "schedule.yaml")
	} else {
		targetInfo, targetBytes, err = requireTargetHash(epicRoot, "schedule.yaml", *expected)
	}
	if err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 1
	}
	candidateValue, issues := input.LoadBytes(candidateBytes, input.Overrides{})
	if len(issues) > 0 {
		fmt.Fprintln(stderr, "NG: 候補を反映しません")
		for _, issue := range issues {
			fmt.Fprintf(stderr, "  - %s\n", issue)
		}
		return 1
	}
	if candidateValue.Epic.ID != epicID {
		fmt.Fprintln(stderr, "NG: 候補の epic.id が対象Epic IDと一致しません")
		return 1
	}
	if !*absent {
		targetValue, issues := input.LoadBytes(targetBytes, input.Overrides{})
		if len(issues) > 0 {
			fmt.Fprintln(stderr, "NG: 対象を読み込めません")
			return 1
		}
		if !sameDate(targetValue.Settings.AsOf, candidateValue.Settings.AsOf) && !*allowAsOfChange {
			fmt.Fprintln(stderr, "NG: settings.as_of を変えるには --allow-as-of-change が必要です")
			return 1
		}
	}

	candidateBytes, err = requireCandidateHash(epicRoot, candidate, candidateDigest)
	if err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 1
	}
	if *absent {
		err = requireAbsent(epicRoot, "schedule.yaml")
	} else {
		targetInfo, _, err = requireTargetHash(epicRoot, "schedule.yaml", *expected)
	}
	if err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 1
	}
	mode := os.FileMode(0o600)
	if targetInfo != nil {
		mode = targetInfo.Mode().Perm()
	}
	temporary, temporaryName, err := createTemporary(epicRoot, mode)
	if err != nil {
		fmt.Fprintf(stderr, "NG: 一時ファイルを作成できません: %v\n", err)
		return 1
	}
	defer epicRoot.Remove(temporaryName)
	if written, err := temporary.Write(candidateBytes); err != nil || written != len(candidateBytes) {
		temporary.Close()
		if err == nil {
			err = io.ErrShortWrite
		}
		fmt.Fprintf(stderr, "NG: 一時ファイルへ書き込めません: %v\n", err)
		return 1
	}
	if err := temporary.Close(); err != nil {
		fmt.Fprintf(stderr, "NG: 一時ファイルを閉じられません: %v\n", err)
		return 1
	}

	if _, err := requireCandidateHash(epicRoot, candidate, candidateDigest); err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 1
	}
	if *absent {
		err = requireAbsent(epicRoot, "schedule.yaml")
	} else {
		_, _, err = requireTargetHash(epicRoot, "schedule.yaml", *expected)
	}
	if err != nil {
		fmt.Fprintf(stderr, "NG: %v\n", err)
		return 1
	}
	if err := epicRoot.Rename(temporaryName, "schedule.yaml"); err != nil {
		fmt.Fprintf(stderr, "NG: 候補を反映できません: %v\n", err)
		return 1
	}
	if err := epicRoot.Remove(candidate); err != nil {
		fmt.Fprintf(stderr, "警告: 反映済み候補を削除できません: %v\n", err)
	}
	fmt.Fprintf(stdout, "OK: %s に反映しました\n", target)
	return 0
}

func applyPaths(targetArg, candidateArg string) (*os.Root, string, string, string, error) {
	target, err := filepath.Abs(targetArg)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("対象パスを解決できません: %w", err)
	}
	target = filepath.Clean(target)
	repositoryPath, err := repositoryRoot(filepath.Dir(target))
	if err != nil {
		return nil, "", "", "", err
	}
	candidatePath, err := filepath.Abs(candidateArg)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("候補パスを解決できません: %w", err)
	}
	candidatePath = filepath.Clean(candidatePath)
	targetRelative, err := filepath.Rel(repositoryPath, target)
	if err != nil || !validTargetRelative(targetRelative) {
		return nil, "", "", "", errors.New("対象はリポジトリ内の epics/<EPIC-ID>/schedule.yaml にしてください")
	}
	candidateRelative, err := filepath.Rel(repositoryPath, candidatePath)
	if err != nil || filepath.Dir(candidateRelative) != filepath.Dir(targetRelative) || !strings.HasPrefix(filepath.Base(candidateRelative), ".schedule.yaml.candidate-") {
		return nil, "", "", "", errors.New("候補は対象と同じディレクトリの .schedule.yaml.candidate- で始まるファイルにしてください")
	}
	repository, err := os.OpenRoot(repositoryPath)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("リポジトリを開けません: %w", err)
	}
	// symbolic link の明示検査はepicディレクトリ、target、candidateの3要素に限り、
	// リポジトリルートからepicディレクトリまでの中間パス要素は検査しない。
	epicRelative := filepath.Dir(targetRelative)
	epicInfo, err := repository.Lstat(epicRelative)
	if err != nil || !epicInfo.IsDir() || epicInfo.Mode()&os.ModeSymlink != 0 {
		repository.Close()
		return nil, "", "", "", errors.New("対象ディレクトリに symbolic link は使えません")
	}
	if info, err := repository.Lstat(targetRelative); err == nil && info.Mode()&os.ModeSymlink != 0 {
		repository.Close()
		return nil, "", "", "", errors.New("対象ファイルに symbolic link は使えません")
	}
	if info, err := repository.Lstat(candidateRelative); err == nil && info.Mode()&os.ModeSymlink != 0 {
		repository.Close()
		return nil, "", "", "", errors.New("候補ファイルに symbolic link は使えません")
	}
	epicRoot, err := repository.OpenRoot(epicRelative)
	repository.Close()
	if err != nil {
		return nil, "", "", "", fmt.Errorf("対象ディレクトリを開けません: %w", err)
	}
	return epicRoot, filepath.Base(epicRelative), target, filepath.Base(candidateRelative), nil
}

func repositoryRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("対象ディレクトリを解決できません: %w", err)
	}
	for {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("対象を含むリポジトリが見つかりません")
		}
		current = parent
	}
}

func validTargetRelative(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	return len(parts) == 3 && parts[0] == "epics" && parts[1] != "" && parts[2] == "schedule.yaml"
}

func readRegularFile(root *os.Root, name string) ([]byte, string, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", errors.New("通常ファイルではありません")
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func requireCandidateHash(root *os.Root, candidate, expected string) ([]byte, error) {
	data, actual, err := readRegularFile(root, candidate)
	if err != nil {
		return nil, fmt.Errorf("候補ファイルを読み込めません: %w", err)
	}
	if actual != expected {
		return nil, errors.New("候補が検証中に変わっています")
	}
	return data, nil
}

func requireTargetHash(root *os.Root, target, expected string) (os.FileInfo, []byte, error) {
	data, actual, err := readRegularFile(root, target)
	if err != nil {
		return nil, nil, fmt.Errorf("対象を読み込めません: %w", err)
	}
	if actual != expected {
		return nil, nil, errors.New("対象が候補作成時から変わっています")
	}
	info, err := root.Lstat(target)
	if err != nil {
		return nil, nil, fmt.Errorf("対象を読み込めません: %w", err)
	}
	return info, data, nil
}

func requireAbsent(root *os.Root, name string) error {
	_, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("対象を確認できません: %w", err)
	}
	return errors.New("新規作成対象が既に存在します")
}

func createTemporary(root *os.Root, mode os.FileMode) (*os.File, string, error) {
	for range 3 {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		name := ".schedule.yaml.apply-" + hex.EncodeToString(random)
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if err := file.Chmod(mode); err != nil {
			file.Close()
			root.Remove(name)
			return nil, "", err
		}
		return file, name, nil
	}
	return nil, "", errors.New("一時ファイル名を作成できません")
}

func sameDate(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}
