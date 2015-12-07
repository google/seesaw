// Copyright 2012 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Author: jsing@google.com (Joel Sing)

// The seesaw_healthcheck binary implements the Seesaw Healthcheck component
// and is responsible for managing the configuration, scheduling and invocation
// of healthchecks against services and backends.
package main

import (
	"flag"

	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/healthcheck"
)

var (
	batchDelay = flag.Duration("batch_delay",
		healthcheck.DefaultServerConfig().BatchDelay,
		"The maximum time to wait for batch to fill before sending the batch to the engine")

	batchSize = flag.Int("batch_size",
		healthcheck.DefaultServerConfig().BatchSize,
		"The maximum number of notifications to include in a single RPC call to the engine")

	channelSize = flag.Int("channel_size",
		healthcheck.DefaultServerConfig().ChannelSize,
		"The size of the notification channel")

	engineSocket = flag.String("engine",
		healthcheck.DefaultServerConfig().EngineSocket,
		"Seesaw Engine Socket")

	maxFailures = flag.Int("max_failures",
		healthcheck.DefaultServerConfig().MaxFailures,
		"The maximum number of consecutive notification failures")

	retryDelay = flag.Duration("retry_delay",
		healthcheck.DefaultServerConfig().RetryDelay,
		"The time between notification RPC retries")
)

func main() {
	flag.Parse()

	cfg := healthcheck.DefaultServerConfig()

	cfg.BatchSize = *batchSize
	cfg.ChannelSize = *channelSize
	cfg.EngineSocket = *engineSocket
	cfg.MaxFailures = *maxFailures
	cfg.RetryDelay = *retryDelay

	hc := healthcheck.NewServer(&cfg)
	server.ShutdownHandler(hc)
	hc.Run()
}
