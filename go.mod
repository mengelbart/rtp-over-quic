module github.com/mengelbart/rtp-over-quic

go 1.18

require (
	github.com/lucas-clemente/quic-go v0.28.1
	github.com/mengelbart/gst-go v0.0.0-20220824124234-80d4e9fbbda6
	github.com/mengelbart/scream-go v0.4.1-0.20220916152424-a421761640a2
	github.com/mengelbart/syncodec v0.0.0-20220105132658-94ec57e63a65
	github.com/pion/interceptor v0.1.12
	github.com/pion/logging v0.2.2
	github.com/pion/rtcp v1.2.10
	github.com/pion/rtp v1.7.13
	github.com/pion/webrtc/v3 v3.1.43
	github.com/spf13/cobra v1.3.0
	golang.org/x/sys v0.0.0-20220622161953-175b2fd9d664
)

require (
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-task/slim-sprig v0.0.0-20210107165309-348f09dbbbc0 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/marten-seemann/qtls-go1-18 v0.1.2 // indirect
	github.com/marten-seemann/qtls-go1-19 v0.1.0 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/crypto v0.0.0-20220516162934-403b01795ae8 // indirect
	golang.org/x/exp v0.0.0-20220722155223-a9213eeb770e // indirect
	golang.org/x/mod v0.6.0-dev.0.20220106191415-9b9b3d81d5e3 // indirect
	golang.org/x/net v0.0.0-20220630215102-69896b714898 // indirect
	golang.org/x/tools v0.1.10 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

replace github.com/lucas-clemente/quic-go v0.28.1 => github.com/mengelbart/quic-go v0.7.1-0.20220909152858-db1304569a92

replace github.com/pion/rtp v1.7.13 => github.com/mengelbart/rtp v1.7.14-0.20220728010821-271390af6fab
