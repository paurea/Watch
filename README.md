# Watch
Portable reimplementation of https://github.com/rsc/rsc/blob/master/cmd/Watch using fsnotify.
It is a command to run inside acme.
Added a regexp as first parameter for the files/directories to watch.


`Watch regexp cmd [args]`

To install it:

`go get -u https://github.com/paurea/Watch`
