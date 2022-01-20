module github.com/mengelbart/rtp-over-quic

go 1.17

require (
	github.com/lucas-clemente/quic-go v0.24.0
	github.com/mengelbart/gst-go v0.0.0-20220120182515-2e3d084f5ff7
	github.com/mengelbart/scream-go v0.3.0
	github.com/mengelbart/syncodec v0.0.0-20220105132658-94ec57e63a65
	github.com/pion/interceptor v0.1.6
	github.com/pion/interceptor/scream v0.1.5
	github.com/pion/rtcp v1.2.9
	github.com/pion/rtp v1.7.4
	github.com/spf13/cobra v1.3.0
)

require (
	github.com/cheekybits/genny v1.0.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-task/slim-sprig v0.0.0-20210107165309-348f09dbbbc0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/marten-seemann/qtls-go1-16 v0.1.4 // indirect
	github.com/marten-seemann/qtls-go1-17 v0.1.0 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/onsi/ginkgo v1.16.4 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/crypto v0.0.0-20210817164053-32db794688a5 // indirect
	golang.org/x/mod v0.5.0 // indirect
	golang.org/x/net v0.0.0-20210813160813-60bc85c4be6d // indirect
	golang.org/x/sys v0.0.0-20211205182925-97ca703d548d // indirect
	golang.org/x/tools v0.1.5 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

replace github.com/lucas-clemente/quic-go v0.24.0 => github.com/mengelbart/quic-go v0.7.1-0.20220112135540-cee0041361fb

//replace github.com/lucas-clemente/quic-go v0.24.0 => ../quic-go

replace github.com/pion/interceptor/scream v0.1.5 => github.com/pion/interceptor v0.1.6-0.20220112135945-47f08b5055be
