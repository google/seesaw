// Copyright 2015 Google Inc. All Rights Reserved.
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

package netlink

/*
#cgo CFLAGS: -I/usr/include/libnl3

#include <stdint.h>

#include <netlink/netlink.h>

// Forward declaration of Go callback trampoline.
int callback(struct nl_msg *msg, void *arg);

int callbackGateway(struct nl_msg *msg, void *arg) {
	return callback(msg, arg);
}

// Wrapper around the equivalent libnl function to provide a cgo-friendly
// callback arg.
int _nl_socket_modify_cb(struct nl_sock *sk, enum nl_cb_type type,
                         enum nl_cb_kind kind, nl_recvmsg_msg_cb_t func,
                         uintptr_t arg) {
	return nl_socket_modify_cb(sk, type, kind, func, (void*)arg);
}
*/
import "C"
