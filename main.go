package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
)

var version = "3.1"
var usage = `PKGBUILD generator for Golang programs.

Will create PKGBUILD which can be used for building package from specified
repo, including optinal additional files to the package.

Tool also capable of creating simple systemd.service file for starting/stoping
daemon and creating .gitignore file in the build directory.

If you want to include additional files to the package, make sure they are
stored by the path they will be placed in the system after package
installation.
E.g., if you want to include config to the package, place it into
'etc/somename/config.conf' directory.

'go-makepkg' will store generated build, service and additional files in the
'build' directory (by default).

Trivial run is (all files in the directory except generated will be included
into the package):
  go-makepkg "my cool package" git://my-repo-url **/* -B

Note: if you want to create package for the project, which uses sub-directories
for binaries and go-gettable with suffix '...', you should specify that suffix
to repo URL as well, like:
  go-makepkg "gb tool" git://github.com/constabulary/gb/... -B

Usage:
  go-makepkg [options] <desc> <repo> [<file>...]
  go-makepkg -h | --help
  go-makepkg -v | --version

Options:
  -v --version  Show version.
  -h --help     Show this help.
  -s            Create service file and include it to the package.
  -g            Create .gitignore file.
  -B            Run 'makepkg' after creating PKGBUILD.
  -c            Clean up leftover files and folders.
  -n <PKGNAME>  Use specified package name instead of automatically generated
                from <repo> URL.
  -l <LICENSE>  License to use [default: GPL].
  -r <PKGREL>   Specify package release number [default: 1].
  -d <DIR>      Directory to place PKGBUILD [default: build].
  -o <NAME>     File to write PKGBULD [default: PKGBUILD].
  -m <NAME>     Specify maintainer$MAINTAINER.
  -p <VAR>      Pass pkgver to specified global variable using ldflags.
  -D <LIST>     Comma-separated list of runtime package dependencies (depends).
  -M <LIST>     Comma-separated list of make package dependencies (makedepends).
`

type pkgFile struct {
	Path string
	Name string
	Hash string
}

type pkgData struct {
	Maintainer       string
	PkgName          string
	PkgRel           string
	PkgDesc          string
	RepoURL          string
	License          string
	Files            []pkgFile
	Dependencies     []string
	MakeDependencies []string
	Backup           []string
	IsWildcardBuild  bool
	VersionVarName   string
}

type serviceData struct {
	Description string
	ExecName    string
}

func parseCommaList(v interface{}) []string {
	if v == nil {
		return []string{}
	}
	return strings.Split(v.(string), ",")
}

func main() {
	args, err := docopt.Parse(
		replaceUsageDefaults(usage),
		nil, true, "go-makepkg "+version, false, true,
	)
	if err != nil {
		panic(err)
	}

	var (
		description       = args[`<desc>`].(string)
		rawRepoURL        = args[`<repo>`].(string)
		fileList          = args[`<file>`].([]string)
		license           = args[`-l`].(string)
		packageRelease    = args[`-r`].(string)
		dirName           = args[`-d`].(string)
		outputName        = args[`-o`].(string)
		doRunBuild        = args[`-B`].(bool)
		doCleanUp         = args[`-c`].(bool)
		doCreateService   = args[`-s`].(bool)
		doCreateGitignore = args[`-g`].(bool)
		maintainer        = args[`-m`].(string)
		versionVarName, _ = args[`-p`].(string)
		dependencies      = parseCommaList(args[`-D`])
		makeDependencies  = parseCommaList(args[`-M`])
	)

	safeRepoURL, isWildcardBuild := trimWildcardFromRepoURL(rawRepoURL)

	repoURL, err := url.Parse(safeRepoURL)
	if err != nil {
		log.Fatal(err)
	}

	if repoURL.Scheme == "ssh" || repoURL.Scheme == "ssh+git" {
		safeRepoURL = strings.Replace(
			safeRepoURL, repoURL.Scheme, "git+ssh", -1,
		)
	}

	// handle git@github.com:
	if strings.Contains(repoURL.Host, ":") {
		safeRepoURL = strings.Replace(
			safeRepoURL,
			repoURL.Host,
			strings.Replace(repoURL.Host, ":", "/", -1),
			-1,
		)
	}

	packageName := getPackageNameFromRepoURL(safeRepoURL)
	if args[`-n`] != nil {
		packageName = args[`-n`].(string)
	}

	err = createOutputDir(dirName)
	if err != nil {
		log.Fatal(err)
	}

	files, err := prepareFileList(fileList, dirName)
	if err != nil {
		log.Fatal(err)
	}

	err = copyLocalFiles(files, dirName)
	if err != nil {
		log.Fatal(err)
	}

	backup := createBackupList(files)

	if doCreateService {
		serviceName := fmt.Sprintf("%s.service", packageName)
		output, err := os.Create(filepath.Join(
			dirName,
			serviceName,
		))

		if err != nil {
			log.Fatal(err)
		}

		err = createServiceFile(output, serviceData{
			Description: description,
			ExecName:    packageName,
		})

		if err != nil {
			log.Fatal(err)
		}

		hash, err := getFileHash(output.Name())
		if err != nil {
			log.Fatal(err)
		}

		files = append(files, pkgFile{
			Name: serviceName,
			Path: filepath.Join(
				"usr/lib/systemd/system/",
				serviceName,
			),
			Hash: hash,
		})
	}

	output, err := os.Create(filepath.Join(dirName, outputName))
	if err != nil {
		log.Fatal(err)
	}

	err = createPkgbuild(output, pkgData{
		Maintainer:       maintainer,
		PkgName:          packageName,
		PkgRel:           packageRelease,
		RepoURL:          safeRepoURL,
		License:          license,
		PkgDesc:          description,
		Files:            files,
		Backup:           backup,
		IsWildcardBuild:  isWildcardBuild,
		VersionVarName:   versionVarName,
		Dependencies:     dependencies,
		MakeDependencies: makeDependencies,
	})
	if err != nil {
		log.Fatal(err)
	}

	if doCreateGitignore {
		err = createGitignore(dirName, packageName)
		if err != nil {
			log.Fatal(err)
		}
	}

	if doRunBuild {
		err = runBuild(dirName, doCleanUp)
		if err != nil {
			log.Fatal(err)
		}
	}

	if doCleanUp {
		err = cleanUp(dirName, packageName)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func runBuild(dir string, cleanUp bool) error {
	logStep("Running makepkg...")

	args := []string{"-f"}
	if cleanUp {
		args = append(args, "-c")
	}

	cmd := exec.Command("makepkg", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func cleanUp(dir, pkgName string) error {
	return os.RemoveAll(filepath.Join(dir, pkgName))
}

func copyLocalFiles(files []pkgFile, outDir string) error {
	logStep("Preparing local files...")
	for _, file := range files {
		logSubStep("Including file in the package: %s", file.Path)

		targetName := filepath.Join(outDir, file.Name)

		_, err := os.Stat(targetName)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else {
			continue
		}

		err = os.Link(file.Path, targetName)
		if err != nil {
			return err
		}
	}

	return nil
}

func createOutputDir(dirName string) error {
	if _, err := os.Stat(dirName); !os.IsNotExist(err) {
		return err
	}

	err := os.Mkdir(dirName, 0755)
	if err != nil {
		return err
	}

	return nil
}

func createPkgbuild(output io.Writer, data pkgData) error {
	logStep("Creating PKGBUILD...")
	return pkgbuildTemplate.Execute(output, data)
}

func createServiceFile(output io.Writer, data serviceData) error {
	logStep("Creating service file...")
	return serviceTemplate.Execute(output, data)
}

func createGitignore(dirName string, pkgName string) error {
	logStep("Creating .gitignore...")

	ignoreFiles := []string{
		"/*.tar.xz",
		"/pkg",
		"/src",
		"/" + pkgName,
	}

	contents := strings.Join(ignoreFiles, "\n") + "\n"

	return ioutil.WriteFile(
		filepath.Join(dirName, ".gitignore"), []byte(contents), 0644,
	)
}

func prepareFileList(names []string, outDir string) ([]pkgFile, error) {
	files := []pkgFile{}

	for _, name := range names {
		stat, err := os.Stat(name)
		if os.IsExist(err) {
			continue
		}

		if err != nil {
			return nil, err
		}

		if stat.IsDir() {
			continue
		}

		if name == "PKGBUILD" {
			continue
		}

		if strings.HasPrefix(name, outDir) {
			continue
		}

		hash, err := getFileHash(name)
		if err != nil {
			return nil, err
		}

		files = append(files, pkgFile{
			Path: name,
			Name: path.Base(name),
			Hash: hash,
		})
	}

	return files, nil
}

func getFileHash(path string) (string, error) {
	hash := md5.New()
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func createBackupList(files []pkgFile) []string {
	logStep("Checking backup files...")

	backup := []string{}
	for _, file := range files {
		logSubStep("Adding to backup: %s", file.Path)
		if strings.HasPrefix(file.Path, "etc/") {
			backup = append(backup, file.Path)
		}
	}

	return backup
}

func getPackageNameFromRepoURL(repo string) string {
	base := path.Base(repo)
	ext := path.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func trimWildcardFromRepoURL(repo string) (string, bool) {
	safeURL := strings.TrimSuffix(repo, "/...")
	return safeURL, safeURL != repo
}

func logSubStep(msg string, data ...interface{}) {
	fmt.Printf("  \x1b[1;34m-> \x1b[39m%s\n", fmt.Sprintf(msg, data...))
}

func logStep(msg string, data ...interface{}) {
	fmt.Printf("\x1b[1;32m==> \x1b[39m%s\n", fmt.Sprintf(msg, data...))
}

func replaceUsageDefaults(usage string) string {
	maintainer, _ := getMaintainerInfo()
	if maintainer != "" {
		maintainer = " [default: " + maintainer + "]"
	}

	return strings.Replace(usage, "$MAINTAINER", maintainer, -1)
}

func getMaintainerInfo() (string, error) {
	cmdName := exec.Command("git", "config", "--global", "user.name")
	name, err := cmdName.CombinedOutput()
	if err != nil {
		return "", err
	}

	cmdEmail := exec.Command("git", "config", "--global", "user.email")
	email, err := cmdEmail.CombinedOutput()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(name)) +
		" <" + strings.TrimSpace(string(email)) + ">", nil
}
