package cmd

import (
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/controller"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/pion/interceptor"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(sendCmd2)
}

var sendCmd2 = &cobra.Command{
	Use: "send2",
	Run: func(_ *cobra.Command, _ []string) {
		done, err := setupProfiling(
			senderCPUProfile,
			senderGoroutineProfile,
			senderHeapProfile,
			senderAllocsProfile,
			senderBlockProfile,
			senderMutexProfile,
		)
		if err != nil {
			log.Fatal(err)
		}
		defer done()
		if err := startSender2(); err != nil {
			log.Fatal(err)
		}
	},
}

func startSender2() error {
	switch sendTransport {
	case "quic", "quic-dgram":
		return runDgramSender()
	case "quic-stream":
	}
	return nil
}

func runDgramSender() error {
	options := []controller.Option{
		controller.SetAddr(sendAddr),
		controller.SetRTPLogFileName(senderRTPDump),
		controller.SetRTCPLogFileName(senderRTCPDump),
		controller.SetCCLogFileName(ccDump),
		controller.SetQLOGDirName(senderQLOGDir),
		controller.SetRTPCongestionControlAlgorithm(controller.CongestionControlAlgorithmFromString(rtpCC)),
	}
	if sendStream {
		options = append(options, controller.EnableStream())
	}
	if localRFC8888 {
		options = append(options, controller.EnableLocalRFC8888())
	}
	c, err := controller.NewQUICDgramSender(GstreamerSourceFactory(), options...)
	if err != nil {
		return err
	}
	defer c.Close()

	errCh := make(chan error)
	go func() {
		if err := c.Start(); err != nil {
			errCh <- err
		}
	}()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigs:
		return nil
	}
}

type mediaSourceFactoryFunc func(interceptor.RTPWriter) (controller.MediaSource, error)

func (f mediaSourceFactoryFunc) Create(w interceptor.RTPWriter) (controller.MediaSource, error) {
	return f(w)
}

func GstreamerSourceFactory() controller.MediaSourceFactory {
	return mediaSourceFactoryFunc(func(w interceptor.RTPWriter) (controller.MediaSource, error) {
		return media.NewGstreamerSource(w)
	})
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
