go-makepkg
==========

Tool for fast packaging Go-lang programs under the archlinux.

It will automatically generate appropriate PKGBUILD and systemd.service files.

How to use
==========

0. `go get github.com/seletskiy/go-makepkg`;
1. `mkdir some-directory`;
2. `cd some-directory`;
3. `mkdir -p etc/mycoolprog/`;
4. Copy any other required files for you program, like config files:  
   `cp <somepath>/example.conf etc/mycoolprog/main.conf`;
5. Omit `-s` flag if you do not want service file:  
   `go-makepkg -sB "my description" git://url-to-prog/repo.git **/*`;
6. Package is ready for install and located at `build/<blah>.tar.xz`;

Typical invocation
==================

```
go-makepkg -gsB "my cool package" git://github.com/seletskiy/go-makepkg *
```

Will generate .gitignore, PKGBUILD and .service file for specified repo (e.g.
go-makepkg) and include all files under current directory to the package.

If you do not want to build package automatically, omit `-B` flag.

See `go-makepkg -h` for more info.

`go-makepkg` by itself can be packaged using itself:  
`go-makepkg -B "go-makepkg tool" git://github.com/seletskiy/go-makepkg.git`
