# OmniMirror

[![CI](https://github.com/mike42/omnimirror/actions/workflows/ci.yml/badge.svg)](https://github.com/mike42/omnimirror/actions/workflows/ci.yml)

Notes and snippets on how different types of software repositories / dependency managers work.

## Ideas to look at

Popular programming languages according to [StackOverflow 2024](https://survey.stackoverflow.co/2024/technology#2-programming-scripting-and-markup-languages), noting down typical dependency manager and public repository.

- Python - `pip` to https://pypi.org/
- JavaScript - `npm` and https://www.npmjs.com/
- ~~SQL~~
- ~~HTML/CSS~~
- ~~TypeScript~~ - would use NPM
- Rust - `cargo` to https://crates.io/
- Go - built-in, mostly hosted via https://pkg.go.dev/
- ~~Bash/Shell~~
- C# - `nuget` and https://www.nuget.org/
- C++ - maybe `vcpkg` or `conan`, more research required.
- Java - Maven/Gradle and https://central.sonatype.com/
- ~~C~~ - seems C++ dependency manager do C as well.
- ~~Kotlin~~ - Same dependency management as Java.
- PHP - `composer` and https://packagist.org/
- ~~PowerShell~~ - PowerShell repos are a thing but its Windows-specific.
- Swift - SPM and https://swiftpackageindex.com/
- Dart - `pub` and https://pub.dev/
- Zig - built-in, no central repository.
- Lua - LuaRocks and https://luarocks.org/ - unsure how widespread it is.
- ~~Assembly~~
- Elixir - `hex` and https://hex.pm/
- Ruby - `gem` and https://rubygems.org/

Popular mirrors on [mirror.aarnet.edu.au](https://mirror.aarnet.edu.au/):

- CentOS
- CPAN/Perl
- Fedora
- FreeBSD
- Kali Linux
- kernel.org
- Linux Mint
- MacPorts
- NetBSD
- OpenBSD
- openSUSE
- Ubuntu

Tools included in ARM Ubuntu GitHub runners: [arm-ubuntu-24-image.md](https://github.com/actions/partner-runner-images/blob/main/images/arm-ubuntu-24-image.md)

Other things to think about:

- Ubuntu PPA's
- Docker
- Flatpak
- Snap

## apt

- Ecosystem: Debian, Ubuntu, etc
- Terminology to check: Architecture, Repository, Release, Distribution, Component, Suite
  - Debian has a Glossary: https://wiki.debian.org/Glossary
- Mirror lists for important repositories:
  - https://www.debian.org/mirror/list
  - https://launchpad.net/ubuntu/+archivemirrors
- Alternative repositories
  - Ubuntu Launchpad PPA's
  - Various vendor-published repos
- Download strategies:
  - rsync
  - recursive wget
  - ftpsync scripts
  - [apt-mirror](https://github.com/apt-mirror/)
- External documentation:
  - [Setting up a Debian archive mirror](https://www.debian.org/mirror/ftpmirror) - good background
- Challenges:
  - Finding a mirror and enumerating available releases / repositories / architectures programmatically.
  - Finding out what `oldstable`, `stable`, `testing` currently refer to - can grab metadata and check `codename`.
  - Once you have found version, `Release` file has architectures (eg. all amd64 arm64 armel armhf i386 mips64el mipsel ppc64el s390x) and Components (eg. main contrib non-free-firmware non-free)
  - Mixing of packages for many releases in the `pool/` directory makes it hard to reduce download size - options are to mirror everything, or use a tool which can read metadata for the entries we need
  - Include/exclude of source packages is another thing to consider.
- Opportunities:
  - Preserving signed metadata.
  - apt repos are commonly accessed over HTTP so are cache-friendly.
  - Mirrors commonly contain an `ls-lR.gz` which could be used to figure out eg. file sizes from a file listing before downloading, or
  - `Packages` lists out `sha256` and filename + size.
- Hosting strategies
  - Just files over HTTP
  - Both have web interfaces which show things you might otherwise use `apt-file` and `apt search` for.
    - https://packages.debian.org/search?keywords=gnome-shell
    - https://launchpad.net/ubuntu/+source/gnome-shell
- Debian has popcon which could be used to download the most important parts of the repository first.
  - https://popcon.debian.org/
  - Packages also have a Priority and Section, inclusion as a dependency of a task* package could be a good way to identify useful things
  - If A depends B, then B could be downloaded first also.
- Ubuntu LaunchPad has an API: 
- Ubuntu is eg. `plucky`, then `plucky-backports`, `plucky-proposed`, `plucky-security`, , `plucky-updates`.
- Debian is eg. `bookworm`, then `bookworm-backports-sloppy`, `bookworm-backports`, `bookworm-proposed-updates`, `bookworm-updates`.

## flatpak

- Ecosystem: Various desktop Linux
- Terminology: Flapak has 'Repository', the central one being Flathub.
- Flatpak API is a front-end for an ostree repo
- Existing tool would be [flatpak/flat-manager](https://github.com/flatpak/flat-manager) (requires postgres database)
- `libostree` (has infrequently updated [ostreedev/ostree-go](https://github.com/ostreedev/ostree-go) bindings)
- On Debian at least, GNOME install will have `libostree-1-1` by default.
  - `apt-cache rdepends` shows the chain is `gnome-control-center` -> `malcontent-gui` -> `libmalcontent-ui-1-1` -> `libflatpak0` -> `libostree-1-1`
- `.flatpakref` for apps looks interesting.
- Prior art: https://jrehkemper.de/content/linux/flatpak/setup-a-local-offline-mirror-for-flatpaks/
- Possible approaches: OCI images, create-usb, etc.

## Building docs

The `docs/` directory will hold documentation in markdown format.

The following commands will build the docs as a static HTML site, in the `site/` directory.

```
python3 -m venv venv/
./venv/bin/pip3 install -r requirements_docs.txt
./venv/bin/mkdocs build
```

Or if writing, this will serve the docs locally and reload them on change.

```
./venv/bin/mkdocs serve --open --livereload
```

