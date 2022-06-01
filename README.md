# RTP over QUIC

`rtp-over-quic` is a partial implementation of [RTP over QUIC](https://datatracker.ietf.org/doc/html/draft-engelbart-rtp-over-quic).

## Features

The application can act as RTP sender or receiver and supports different transport options:

* Transport protocol:
  * UDP
  * QUIC Datagrams
  * (TCP)
* Real-time congestion control: SCReAM, (GCC), None
* RTCP:
  * RFC 8888, optionally generated by the sender using QUIC statistics (RFC 8888 is required for SCReAM)
  * TWCC (required for GCC)
* Codec: `h264`, `vp8`, `vp9`
* QUIC congestion control: NewReno, None
* Optionally send non-RTP data on a QUIC stream
* Various logging options for RTP/RTCP, QLOG, congestion control statistics

The implementation uses [Gstreamer](https://gstreamer.freedesktop.org/) for video coding and RTP (de-)packetization and CGO to integrate [SCReAM](https://github.com/EricssonResearch/scream/).

## Build and Run

After installing the dependencies (Gstreamer, C/C++ Compiler) and building with `go build`, you can start a receiver with `./rtp-over-quic receive` and a sender with `./rtp-over-quic send`.
Use the `-h` flag to see the available options for receiver and sender.
