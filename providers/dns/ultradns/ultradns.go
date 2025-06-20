// Package ultradns implements a DNS provider for solving the DNS-01 challenge using ultradns.
package ultradns

import (
	"errors"
	"fmt"
	"time"
	// "encoding/json"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/platform/config/env"
	"github.com/go-acme/lego/v4/providers/dns/internal/useragent"
	"github.com/ultradns/ultradns-go-sdk/pkg/client"
	"github.com/ultradns/ultradns-go-sdk/pkg/record"
	"github.com/ultradns/ultradns-go-sdk/pkg/rrset"
	"github.com/ultradns/ultradns-go-sdk/pkg/zone"
)

// Environment variables names.
const (
	envNamespace = "ULTRADNS_"

	EnvUsername = envNamespace + "USERNAME"
	EnvPassword = envNamespace + "PASSWORD"
	EnvEndpoint = envNamespace + "ENDPOINT"

	EnvTTL                = envNamespace + "TTL"
	EnvPropagationTimeout = envNamespace + "PROPAGATION_TIMEOUT"
	EnvPollingInterval    = envNamespace + "POLLING_INTERVAL"
)

const defaultEndpoint = "https://api.ultradns.com/"

var _ challenge.ProviderTimeout = (*DNSProvider)(nil)

// DNSProvider implements the challenge.Provider interface.
type DNSProvider struct {
	config *Config
	client *client.Client
}

// Config is used to configure the creation of the DNSProvider.
type Config struct {
	Username string
	Password string
	Endpoint string

	TTL                int
	PropagationTimeout time.Duration
	PollingInterval    time.Duration
}

// NewDefaultConfig returns a default configuration for the DNSProvider.
func NewDefaultConfig() *Config {
	return &Config{
		Endpoint:           env.GetOrDefaultString(EnvEndpoint, defaultEndpoint),
		TTL:                env.GetOrDefaultInt(EnvTTL, dns01.DefaultTTL),
		PropagationTimeout: env.GetOrDefaultSecond(EnvPropagationTimeout, 2*time.Minute),
		PollingInterval:    env.GetOrDefaultSecond(EnvPollingInterval, 4*time.Second),
	}
}

// NewDNSProvider returns a DNSProvider instance configured for ultradns.
// Credentials must be passed in the environment variables:
// ULTRADNS_USERNAME and ULTRADNS_PASSWORD.
func NewDNSProvider() (*DNSProvider, error) {
	values, err := env.Get(EnvUsername, EnvPassword)
	if err != nil {
		return nil, fmt.Errorf("ultradns: %w", err)
	}

	config := NewDefaultConfig()
	config.Username = values[EnvUsername]
	config.Password = values[EnvPassword]

	return NewDNSProviderConfig(config)
}

// NewDNSProviderConfig return a DNSProvider instance configured for ultradns.
func NewDNSProviderConfig(config *Config) (*DNSProvider, error) {
	if config == nil {
		return nil, errors.New("ultradns: the configuration of the DNS provider is nil")
	}

	ultraConfig := client.Config{
		Username:  config.Username,
		Password:  config.Password,
		HostURL:   config.Endpoint,
		UserAgent: useragent.Get(),
	}

	uClient, err := client.NewClient(ultraConfig)
	if err != nil {
		return nil, fmt.Errorf("ultradns: %w", err)
	}

	return &DNSProvider{config: config, client: uClient}, nil
}

// Timeout returns the timeout and interval to use when checking for DNS propagation.
func (d *DNSProvider) Timeout() (timeout, interval time.Duration) {
	return d.config.PropagationTimeout, d.config.PollingInterval
}

// Present creates a TXT record using the specified parameters.
func (d *DNSProvider) Present(domain, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)

	authZone, err := dns01.FindZoneByFqdn(info.EffectiveFQDN)

	if err != nil {
	 	return fmt.Errorf("ultradns: could not find zone for domain %q: %w", domain, err)
	}

	zoneService,err := zone.Get(d.client)
	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	} 

	_, resZone, err := zoneService.ReadZone(authZone)

	zoneOrAlias := authZone
	EffectiveFQDN := info.EffectiveFQDN

	if resZone.OriginalZoneName != "" {
		zoneOrAlias = resZone.OriginalZoneName
		EffectiveFQDN = "_acme-challenge." + zoneOrAlias
	} 

	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	rrSetKeyData := &rrset.RRSetKey{
		Owner:      EffectiveFQDN,
		Zone:       zoneOrAlias,
		RecordType: "TXT",
	}

	rrSetData := &rrset.RRSet{
		OwnerName: zoneOrAlias,
		TTL:       d.config.TTL,
		RRType:    "TXT",
		RData:     []string{info.Value},
	}

	recordService, err := record.Get(d.client)
	resRecordCode, _, _ := recordService.Read(rrSetKeyData)

	if resRecordCode != nil && resRecordCode.StatusCode == 200 {
		_, err = recordService.Update(rrSetKeyData, rrSetData)
	} else {
		_, err = recordService.Create(rrSetKeyData, rrSetData)
	}
	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	return nil
}

// CleanUp removes the TXT record matching the specified parameters.
func (d *DNSProvider) CleanUp(domain, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)

	authZone, err := dns01.FindZoneByFqdn(info.EffectiveFQDN)
	if err != nil {
		return fmt.Errorf("ultradns: could not find zone for domain %q: %w", domain, err)
	}

	zoneService,err := zone.Get(d.client)
	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	_, resZone, err := zoneService.ReadZone(authZone)

	zoneOrAlias := authZone
	EffectiveFQDN := info.EffectiveFQDN

	if resZone.OriginalZoneName != "" {
		zoneOrAlias = resZone.OriginalZoneName
		EffectiveFQDN = "_acme-challenge." + zoneOrAlias
	} 

	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	recordService, err := record.Get(d.client)
	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	rrSetKeyData := &rrset.RRSetKey{
		Owner:      EffectiveFQDN,
		Zone:       zoneOrAlias,
		RecordType: "TXT",
	}

	_, err = recordService.Delete(rrSetKeyData)
	if err != nil {
		return fmt.Errorf("ultradns: %w", err)
	}

	return nil
}
