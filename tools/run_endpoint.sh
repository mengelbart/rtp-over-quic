#!/bin/bash

# Set up the routing needed for the simulation.
/setup.sh

if [ "$ROLE" == "sender" ]; then
    echo "Starting RTP over QUIC sender..."
    QUIC_GO_LOG_LEVEL=error ./rtq send -addr $RECEIVER:4242 $ARGS
else
    echo "Running RTP over QUIC receiver."
    QUIC_GO_LOG_LEVEL=error ./rtq receive $ARGS
fi
