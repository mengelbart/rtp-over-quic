package cmd

import (
	"errors"
	"log"
	"os"
	"runtime/pprof"

	"github.com/spf13/cobra"
)

var (
	transport string
	addr      string

	tcpCongAlg string
	quicCC     string

	codec string

	rtpDumpFile  string
	rtcpDumpFile string
	qlogDir      string
	keyLogFile   string

	cpuProfile       string
	goroutineProfile string
	heapProfile      string
	allocsProfile    string
	blockProfile     string
	mutexProfile     string
)

var errInvalidTransport = errors.New("unknown transport protocol")

func init() {
	rootCmd.PersistentFlags().StringVar(&transport, "transport", "quic", "Transport protocol to use: quic, udp or tcp")
	rootCmd.PersistentFlags().StringVarP(&addr, "addr", "a", ":4242", "QUIC server address")

	rootCmd.PersistentFlags().StringVar(&tcpCongAlg, "tcp-congestion", "reno", "TCP Congestion control algorithm to use, only when --transport is tcp")
	rootCmd.PersistentFlags().StringVar(&quicCC, "quic-cc", "none", "QUIC congestion control algorithm. ('none', 'newreno')")

	rootCmd.PersistentFlags().StringVarP(&codec, "codec", "c", "h264", "Media codec")

	rootCmd.PersistentFlags().StringVar(&rtpDumpFile, "rtp-dump", "", "RTP dump file, 'stdout' for Stdout")
	rootCmd.PersistentFlags().StringVar(&rtcpDumpFile, "rtcp-dump", "", "RTCP dump file, 'stdout' for Stdout")
	rootCmd.PersistentFlags().StringVar(&qlogDir, "qlog", "", "QLOG directory. No logs if empty. Use 'sdtout' for Stdout or '<directory>' for a QLOG file named '<directory>/<connection-id>.qlog'")
	rootCmd.PersistentFlags().StringVar(&keyLogFile, "keylogfile", "", "TLS keys for decrypting traffic e.g. using wireshark")

	rootCmd.PersistentFlags().StringVar(&cpuProfile, "pprof-cpu", "", "Create pprof CPU profile with given filename")
	rootCmd.PersistentFlags().StringVar(&goroutineProfile, "pprof-goroutine", "", "Create pprof 'goroutine' profile with given filename")
	rootCmd.PersistentFlags().StringVar(&heapProfile, "pprof-heap", "", "Create pprof 'heap' profile with given filename")
	rootCmd.PersistentFlags().StringVar(&allocsProfile, "pprof-allocs", "", "Create pprof 'allocs' profile with given filename")
	rootCmd.PersistentFlags().StringVar(&blockProfile, "pprof-block", "", "Create pprof 'block' profile with given filename")
	rootCmd.PersistentFlags().StringVar(&mutexProfile, "pprof-mutex", "", "Create pprof 'mutex' profile with given filename")
}

var rootCmd = &cobra.Command{}

func Execute() {
	done, err := setupProfiling(
		cpuProfile,
		goroutineProfile,
		heapProfile,
		allocsProfile,
		blockProfile,
		mutexProfile,
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := done(); err != nil {
			log.Fatal(err)
		}
	}()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func setupProfiling(cpu, goroutine, heap, allocs, block, mutex string) (func() error, error) {
	doneFns := []func() error{}
	concatDoneFns := func(fns []func() error) func() error {
		return func() error {
			for _, f := range fns {
				if err := f(); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if cpu != "" {
		f, err := os.Create(cpu)
		if err != nil {
			return concatDoneFns(nil), err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			return concatDoneFns(nil), err
		}
		doneFns = append(doneFns, func() error {
			pprof.StopCPUProfile()
			return nil
		})
	}
	for k, v := range map[string]string{
		"goroutine": goroutine,
		"heap":      heap,
		"allocs":    allocs,
		"block":     block,
		"mutex":     mutex,
	} {
		if v != "" {
			f, err := os.Create(v)
			if err != nil {
				return concatDoneFns(doneFns), err
			}
			doneFns = append(doneFns, func() error {
				return pprof.Lookup(k).WriteTo(f, 0)
			})
		}
	}
	return concatDoneFns(doneFns), nil
}
