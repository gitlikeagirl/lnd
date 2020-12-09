#!/bin/bash

function waitnoerror() {
        for i in {1..30}; do $@ && return; sleep 1; done
        echo "timeout"
        exit 1
}
