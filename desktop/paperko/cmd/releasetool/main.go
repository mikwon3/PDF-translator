// releasetool 은 PaperKo 릴리스를 만드는 데 쓰는 작은 도구다(scripts/release.sh 가 부른다).
//
//	releasetool keygen   -key <파일>                       서명 키를 만들고 공개키를 적는다
//	releasetool manifest -version 1.8.4 -notes notes.md -out manifest.json <설치본…>
//	releasetool sign     -key <파일> manifest.json          manifest.json.sig 를 만든다
//	releasetool verify   manifest.json                      앱에 심은 공개키로 서명을 확인한다
//
// 개인키는 **저장소 밖**(기본: 사용자 설정 폴더)에만 둔다. 잃어버리면 이미 설치된 앱이 받아들일
// 새 판을 더는 만들 수 없으므로 따로 안전하게 보관한다.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paperko/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "manifest":
		err = manifest(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasetool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "releasetool keygen|manifest|sign|verify …")
	os.Exit(2)
}

func defaultKey() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "PaperKo", "release-key", "paperko-release.key")
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	keyPath := fs.String("key", defaultKey(), "개인키 파일")
	_ = fs.Parse(args)
	if _, err := os.Stat(*keyPath); err == nil {
		return fmt.Errorf("이미 키가 있습니다 — 덮어쓰지 않습니다: %s", *keyPath)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*keyPath), 0o700); err != nil {
		return err
	}
	seed := base64.StdEncoding.EncodeToString(priv.Seed())
	if err := os.WriteFile(*keyPath, []byte(seed+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("서명 키를 읽지 못했습니다: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("서명 키 파일이 이상합니다")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// assetOf 는 파일 이름으로 OS·CPU 를 가린다. 패키징 스크립트가 붙이는 이름 규칙을 따른다:
// PaperKo-<ver>-arm64.dmg · PaperKo-<ver>-amd64-installer.exe.
func assetOf(path, macArch string) (update.Asset, error) {
	name := filepath.Base(path)
	a := update.Asset{Name: name}
	switch {
	case strings.HasSuffix(name, ".dmg"):
		a.OS, a.Arch = "darwin", macArch
	case strings.HasSuffix(name, "-amd64-installer.exe"):
		a.OS, a.Arch = "windows", "amd64"
	case strings.HasSuffix(name, "-arm64-installer.exe"):
		a.OS, a.Arch = "windows", "arm64"
	default:
		return a, fmt.Errorf("어느 OS 의 설치본인지 이름으로 알 수 없습니다: %s", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return a, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return a, err
	}
	a.SHA256, a.Size = hex.EncodeToString(h.Sum(nil)), n
	return a, nil
}

func manifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	ver := fs.String("version", "", "판 번호 (예: 1.8.4)")
	notes := fs.String("notes", "", "릴리스 설명 파일(Markdown)")
	out := fs.String("out", update.ManifestName, "쓸 파일")
	macArch := fs.String("mac-arch", "arm64", "DMG 의 CPU")
	_ = fs.Parse(args)
	if *ver == "" || fs.NArg() == 0 {
		return errors.New("-version 과 설치본 파일이 필요합니다")
	}
	m := update.Manifest{Version: strings.TrimPrefix(*ver, "v"), Published: time.Now().UTC().Format(time.RFC3339)}
	if *notes != "" {
		data, err := os.ReadFile(*notes)
		if err != nil {
			return err
		}
		m.Notes = strings.TrimSpace(string(data))
	}
	for _, p := range fs.Args() {
		a, err := assetOf(p, *macArch)
		if err != nil {
			return err
		}
		m.Assets = append(m.Assets, a)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, append(data, '\n'), 0o644)
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", defaultKey(), "개인키 파일")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("서명할 manifest 파일 하나가 필요합니다")
	}
	priv, err := loadKey(*keyPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	// 앱에 심은 공개키와 짝이 맞는 키인지 먼저 본다 — 다른 키로 서명하면 앱이 모두 거절한다.
	if !priv.Public().(ed25519.PublicKey).Equal(update.PublicKey()) {
		return errors.New("이 키는 앱에 심은 공개키와 짝이 아닙니다")
	}
	return os.WriteFile(fs.Arg(0)+".sig", []byte(update.Sign(data, priv)+"\n"), 0o644)
}

func verify(args []string) error {
	if len(args) != 1 {
		return errors.New("manifest 파일 하나가 필요합니다")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(args[0] + ".sig")
	if err != nil {
		return err
	}
	m, err := update.VerifyManifest(data, sig, update.PublicKey())
	if err != nil {
		return err
	}
	fmt.Printf("서명 확인: %s (설치본 %d개)\n", m.Version, len(m.Assets))
	return nil
}
