package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
)

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
  -n=<PKGNAME>  Use specified package name instead of automatically generated
                from <repo> URL.
  -l=<LICENSE>  License to use [default: GPL].
  -r=<PKGREL>   Specify package release number [default: 1].
  -d=<DIR>      Directory to place PKGBUILD [default: build].
  -o=<NAME>     File to write PKGBULD [default: PKGBUILD].
`

type pkgFile struct {
	Path string
	Name string
	Hash string
}

type pkgData struct {
	PkgName string
	PkgRel  string
	PkgDesc string
	RepoURL string
	License string
	Files   []pkgFile
	Backup  []string
}

type serviceData struct {
	Description string
	ExecName    string
}

func main() {
	args, err := docopt.Parse(usage, nil, true, "go-makepkg 2.1", false, true)
	if err != nil {
		panic(err)
	}

	var (
		description       = args[`<desc>`].(string)
		repoURL           = args[`<repo>`].(string)
		fileList          = args[`<file>`].([]string)
		license           = args[`-l`].(string)
		packageRelease    = args[`-r`].(string)
		dirName           = args[`-d`].(string)
		outputName        = args[`-o`].(string)
		doRunBuild        = args[`-B`].(bool)
		doCleanUp         = args[`-c`].(bool)
		doCreateService   = args[`-s`].(bool)
		doCreateGitignore = args[`-g`].(bool)
	)

	packageName := getPackageNameFromRepoURL(repoURL)
	if args[`-n`] != nil {
		packageName = args[`-n`].(string)
	}

	err = createOutputDir(dirName)
	if err != nil {
		panic(err)
	}

	files, err := prepareFileList(fileList, dirName)
	if err != nil {
		panic(err)
	}

	err = copyLocalFiles(files, dirName)
	if err != nil {
		panic(err)
	}

	backup := createBackupList(files)

	if doCreateService {
		serviceName := fmt.Sprintf("%s.service", packageName)
		output, err := os.Create(filepath.Join(
			dirName,
			serviceName,
		))

		if err != nil {
			panic(err)
		}

		err = createServiceFile(output, serviceData{
			Description: description,
			ExecName:    packageName,
		})

		if err != nil {
			panic(err)
		}

		hash, err := getFileHash(output.Name())
		if err != nil {
			panic(err)
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
		panic(err)
	}

	err = createPkgbuild(output, pkgData{
		PkgName: packageName,
		PkgRel:  packageRelease,
		RepoURL: repoURL,
		License: license,
		PkgDesc: description,
		Files:   files,
		Backup:  backup,
	})
	if err != nil {
		panic(err)
	}

	if doCreateGitignore {
		err = createGitignore(dirName, packageName)
		if err != nil {
			panic(err)
		}
	}

	if doRunBuild {
		err = runBuild(dirName, doCleanUp)
		if err != nil {
			panic(err)
		}
	}

	if doCleanUp {
		err = cleanUp(dirName, packageName)
		if err != nil {
			panic(err)
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

func logSubStep(msg string, data ...interface{}) {
	fmt.Printf("  \x1b[1;34m-> \x1b[39m%s\n", fmt.Sprintf(msg, data...))
}

func logStep(msg string, data ...interface{}) {
	fmt.Printf("\x1b[1;32m==> \x1b[39m%s\n", fmt.Sprintf(msg, data...))
}
