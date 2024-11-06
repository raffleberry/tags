# tags
Use taglib in Go

## Usage
#### Windows
- install `gcc` (tdm or msys or ...)
- run `source source.bash` (for windows)
- use `go get github.com/raffleberry/tags`
- run your app

## Building libs/binaries

#### Windows(Msys2)
```sh
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-pkg-config mingw-w64-x86_64-taglib
```
#### Linux
```sh
# ubuntu
git clone https://github.com/taglib/taglib
sudo apt install gcc g++ cmake make libutfcpp-dev zlib1g-dev

# (Static builds)
./bake taglib
```
test the cli program `build/tags` to double check if it worked.
```sh
go get github.com/raffleberry/tags
```



## Debugging
```sh
./bake clean
```
clean before rebuilds

## Docs
[https://pkg.go.dev/github.com/raffleberry/tags](https://pkg.go.dev/github.com/raffleberry/tags)
