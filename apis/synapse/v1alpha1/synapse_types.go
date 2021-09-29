/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SynapseSpec defines the desired state of Synapse
type SynapseSpec struct {

	// +kubebuilder:validation:Required

	// Runtime configuration for Synapse and settings related to the Matrix protocol
	Homeserver SynapseSpecHomeserver `json:"homeserver"`
}

type SynapseSpecHomeserver struct {

	// +kubebuilder:default:=example.com

	// Domain name of the server
	// This is not necessarily the host name where the service is reachable. In fact, you may want to omit any subdomains
	// from this value as the server name set here will be the name of your homeserver in the fediverse, and will be the
	// domain name at the end of every user's username
	ServerName string `json:"serverName,omitempty"`

	// +kubebuilder:default:=false

	// Enable anonymous telemetry to matrix.org
	Telemetry bool `json:"telemetry,omitempty"`

	// +kubebuilder:default:=true

	// Set to false to disable presence (online/offline indicators)
	Presence bool `json:"presence,omitempty"`

	// +kubebuilder:default:=false

	// Set to true to block non-admins from inviting users to any rooms
	BlockNonAdminInvites bool `json:"blockNonAdminInvites,omitempty"`

	// +kubebuilder:default:=true

	// Set to false to disable message searching
	Search bool `json:"search,omitempty"`

	// +kubebuilder:default:=invite

	// Which types of rooms to enable end-to-end encryption on by default
	// off: none
	// invite: private messages, or rooms created with the private_chat or trusted_private_chat room preset
	// all: all rooms
	EncryptByDefault string `json:"encryptByDefault,omitempty"`

	// +kubebuilder:default:=admin@example.com

	// Email address of the administrator
	AdminEmail string `json:"adminEmail,omitempty"`

	// Settings related to image and multimedia uploads
	Uploads SynapseSpecHomeServerUploads `json:"uploads,omitempty"`

	// User registration settings
	Registration SynapseSpecHomeServerRegistration `json:"registration,omitempty"`

	// Settings for the URL preview crawler
	UrlPreviews SynapseSpecHomeServerUrlPreview `json:"urlPreviews,omitempty"`

	// +kubebuilder:default:="7d"

	// How long to keep redacted events in unredacted form in the database
	RetentionPeriod string `json:"retentionPeriod,omitempty"`

	// Security settings
	Security SynapseSpecHomeServerSecurity `json:"security,omitempty"`

	// +kubebuilder:default:=false

	// Set to true to globally block access to the homeserver
	Disabled bool `json:"disabled,omitempty"`

	// Human readable reason for why the homeserver is blocked
	DisabledMessage string `json:"disabledMessage,omitempty"`

	// Log configuration for Synapse
	Logging SynapseSpecHomeServerLogging `json:"logging,omitempty"`
}

// Settings related to image and multimedia uploads
type SynapseSpecHomeServerUploads struct {
	// +kubebuilder:default:="10M"

	// Max upload size in bytes
	MaxSize string `json:"maxSize,omitempty"`

	// +kubebuilder:default:="32M"

	// Max image size in pixels
	MaxPixels string `json:"maxPixels,omitempty"`
}

// User registration settings
type SynapseSpecHomeServerRegistration struct {
	// +kubebuilder:default:="10M"

	// Max upload size in bytes
	MaxSize string `json:"maxSize,omitempty"`

	// +kubebuilder:default:="32M"

	// Max image size in pixels
	MaxPixels string `json:"maxPixels,omitempty"`

	// +kubebuilder:default:=false

	// Allow new users to register an account
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:default:=false

	// Allow users to join rooms as a guest
	AllowGuests bool `json:"allowGuests,omitempty"`

	// Rooms to automatically join all new users to
	AutoJoinRooms []string `json:"autoJoinRooms,omitempty"`
}

// Settings for the URL preview crawler
type SynapseSpecHomeServerUrlPreview struct {

	// +kubebuilder:default:=false

	// Enable URL previews.
	// WARNING: Make sure to review the default rules below to ensure that users cannot crawl
	// sensitive internal endpoints in your cluster.
	Enabled bool `json:"enabled,omitempty"`

	// Blacklists and whitelists for the URL preview crawler
	Rules SynapseSpecHomeServerUrlPreviewRules `json:"rules,omitempty"`
}

type SynapseSpecHomeServerUrlPreviewRules struct {

	// +kubebuilder:default:="10M"

	// Maximum size of a crawlable page. Keep this low to prevent a DOS vector
	MaxSize string `json:"maxSize,omitempty"`

	// Whitelist and blacklist for crawlable IP addresses
	Ip SynapseSpecHomeServerUrlPreviewRulesIp `json:"ip,omitempty"`

	// Whitelist and blacklist based on URL pattern matching
	Url SynapseSpecHomeServerUrlPreviewRulesUrl `json:"url,omitempty"`
}

type SynapseSpecHomeServerUrlPreviewRulesIp struct {
	Whitelist []string `json:"whitelist,omitempty"`

	// +kubebuilder:default:={"127.0.0.0/8","10.0.0.0/8","172.16.0.0/12","192.168.0.0/16","100.64.0.0/10","169.254.0.0/16","::1/128","fe80::/64","fc00::/7"}

	Blacklist []string `json:"blacklist,omitempty"`
}

type SynapseSpecHomeServerUrlPreviewRulesUrl struct {
	Whitelist map[string]string `json:"whitelist,omitempty"`

	// blacklist:
	//  // blacklist any URL with a username in its URI
	//  - username: '*'
	//
	//  // blacklist all *.google.com URLs
	//  - netloc: 'google.com'
	//  - netloc: '*.google.com'
	//
	//  // blacklist all plain HTTP URLs
	//  - scheme: 'http'
	//
	//  // blacklist http(s)://www.acme.com/foo
	//  - netloc: 'www.acme.com'
	//    path: '/foo'
	//
	//  // blacklist any URL with a literal IPv4 address
	//  - netloc: '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'
	Blacklist map[string]string `json:"blacklist,omitempty"`
}

// Security settings
type SynapseSpecHomeServerSecurity struct {
	// A secret which is used to sign access tokens. If none is specified,
	// the registration_shared_secret is used, if one is given; otherwise,
	// a secret key is derived from the signing key.
	MacaroonSecretKey string `json:"macaroonSecretKey,omitempty"`

	// +kubebuilder:default:=true

	// This disables the warning that is emitted when the
	// trustedKeyServers include 'matrix.org'. See below.
	// Set to false to re-enable the warning.
	SurpressKeyServerWarning bool `json:"surpressKeyServerWarning,omitempty"`

	// The trusted servers to download signing keys from.
	//
	// When we need to fetch a signing key, each server is tried in parallel.
	//
	// Normally, the connection to the key server is validated via TLS certificates.
	// Additional security can be provided by configuring a `verify key`, which
	// will make synapse check that the response is signed by that key.
	//
	// This setting supercedes an older setting named `perspectives`. The old format
	// is still supported for backwards-compatibility, but it is deprecated.
	//
	// 'trustedKeyServers' defaults to matrix.org, but using it will generate a
	// warning on start-up. To suppress this warning, set
	// 'surpressKeyServerWarning' to true.
	//
	// Options for each entry in the list include:
	//
	//    serverName: the name of the server. required.
	//
	//    verifyKeys: an optional map from key id to base64-encoded public key.
	//
	//       If specified, we will check that the response is signed by at least
	//
	//       one of the given keys.
	//
	//    acceptKeysInsecurely: a boolean. Normally, if `verify_keys` is unset,
	//
	//       and federation_verify_certificates is not `true`, synapse will refuse
	//
	//       to start, because this would allow anyone who can spoof DNS responses
	//
	//       to masquerade as the trusted key server. If you know what you are doing
	//
	//       and are sure that your network environment provides a secure connection
	//
	//       to the key server, you can set this to `true` to override this
	//
	//       behaviour.
	//
	// An example configuration might look like:
	//
	// trustedKeyServers:
	//
	//   - serverName: my_trusted_server.example.com
	//
	//     verifyKeys:
	//
	//       - id: "ed25519:auto"
	//
	//         key: "abcdefghijklmnopqrstuvwxyzabcdefghijklmopqr"
	//
	//     acceptKeysInsecurely: false
	//
	//   - serverName: my_other_trusted_server.example.com
	TrustedKeyServers map[string]string `json:"trustedKeyServers,omitempty"`
}

// Log configuration for Synapse
type SynapseSpecHomeServerLogging struct {
	// +kubebuilder:default:=WARNING
	// +kubebuilder:validation:Enum=DEBUG;INFO;WARNING;CRITICAL

	// Root log level is the default log level for log outputs that do not have more
	// specific settings.
	RootLogLevel string `json:"rootLogLevel,omitempty"`

	// +kubebuilder:default:=WARNING
	// +kubebuilder:validation:Enum:={DEBUG,INFO,WARNING,CRITICAL}

	// beware: increasing this to DEBUG will make synapse log sensitive
	// information such as access tokens.
	SqlLogLevel string `json:"sqlLogLevel,omitempty"`

	// +kubebuilder:default:=WARNING
	// +kubebuilder:validation:Enum:={DEBUG,INFO,WARNING,CRITICAL}

	// The log level for the synapse server
	SynapseLogLevel string `json:"synapseLogLevel,omitempty"`
}

// SynapseStatus defines the observed state of Synapse
type SynapseStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Synapse is the Schema for the synapses API
type Synapse struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SynapseSpec   `json:"spec,omitempty"`
	Status SynapseStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SynapseList contains a list of Synapse
type SynapseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Synapse `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Synapse{}, &SynapseList{})
}
