package rtc

import (
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
)

func gstSrcPipeline(codec string, src string, ssrc uint, initialBitrate uint) (*gstsrc.Pipeline, error) {
	srcPipeline, err := gstsrc.NewPipeline(codec, src)
	if err != nil {
		return nil, err
	}
	srcPipeline.SetSSRC(ssrc)
	srcPipeline.SetBitRate(initialBitrate)
	go srcPipeline.Start()
	return srcPipeline, nil
}

func gstDstPipeline(codec string, dst string) (*gstsink.Pipeline, error) {
	dstPipeline, err := gstsink.NewPipeline(codec, dst)
	if err != nil {
		return nil, err
	}
	dstPipeline.Start()
	return dstPipeline, nil
}
