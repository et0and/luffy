package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultPath is where a PirateBox installation keeps its configuration.
const DefaultPath = "/opt/piratebox/conf/piratebox.conf"

// Config is the typed view over piratebox.conf. Field names mirror the
// original shell variables wherever possible so that the port stays easy to
// audit against the original source.
type Config struct {
	Path   string
	Values Values

	// Folder configuration.
	PirateboxFolder string
	WwwFolder       string
	WwwContent      string
	ShareFolder     string
	UploadFolder    string
	Timesave        string
	TimesaveFormat  string
	NodeConfigPath  string
	VersionFile     string

	// Generated files.
	ChatFile      string
	GenChatFile   string
	GenDiskFile   string
	LeaseFile     string
	HostsFile     string
	HostsMeshFile string
	DNSMasqConf   string
	RadvdConf     string
	AvahiSchema   string
	AvahiConf     string
	InitDone      string

	// Startup behaviour.
	DoIfconfig     bool
	DoIW           bool
	PhyIf          string
	ProbeInterface bool
	ProbeTime      int
	UseAPN         bool
	UseDNSMasq     bool

	// Network.
	Host             string
	Interface        string
	DNSMasqInterface string
	DoBridge         bool
	Bridge           string
	Net              string
	IPShort          string
	StartLease       string
	EndLease         string
	LeaseDuration    string
	Netmask          string

	// IPv6.
	IPv6Enable bool
	IPv6Advert string
	IPv6IP     string
	IPv6Mask   string
	IPv6Prefix string

	// Firewall.
	FirewallFetchDNS  bool
	FirewallFetchHTTP bool

	// Node (mesh).
	NodeConfigActive bool
	NodeName         string
	NodeGen          bool
	NodeInterface    string
	NodeIPv6IP       string
	NodeIPv6Mask     string
	NodeIPv6SetIP    bool

	// Web services.
	User               string
	Group              string
	HTTPAddr           string
	HTTPPort           int
	UploadMaxBytes     int64
	DroopyEnabled      bool
	DroopyPort         string
	DroopyTxt          string
	DroopyChmod        string
	DroopyUserDir      string
	DroopyUseUser      bool
	CustomDirlistCopy  bool
	PbxJSONGeneration  bool
	ShoutboxEnabled    bool
	ShoutboxClientTime bool
	ResetChat          bool
	GlobalChat         bool
	GlobalDest         []string
	EnableIRCServer    bool

	warnings []string
}

// Defaults returns the built-in configuration, matching the shipped
// piratebox.conf from the original project.
func Defaults() Values {
	return Values{
		"PIRATEBOX_FOLDER":          "/opt/piratebox",
		"WWW_FOLDER":                "$PIRATEBOX_FOLDER/www",
		"WWW_CONTENT":               "$PIRATEBOX_FOLDER/share/content",
		"SHARE_FOLDER":              "$PIRATEBOX_FOLDER/share",
		"UPLOADFOLDER":              "$SHARE_FOLDER/Shared",
		"NODE_CONFIG":               "$PIRATEBOX_FOLDER/conf/node.conf",
		"TIMESAVE":                  "$PIRATEBOX_FOLDER/share/timesave_file",
		"TIMESAVE_FORMAT":           "+%C%g%m%d %H%M",
		"DO_IFCONFIG":               "yes",
		"DO_IW":                     "no",
		"PHY_IF":                    "phy0",
		"PROBE_INTERFACE":           "no",
		"PROBE_TIME":                "10",
		"USE_APN":                   "yes",
		"USE_DNSMASQ":               "yes",
		"HOST":                      "piratebox.lan",
		"INTERFACE":                 "wlan0",
		"DNSMASQ_INTERFACE":         "wlan0",
		"DO_BRIDGE":                 "no",
		"BRIDGE":                    "br-lan",
		"NET":                       "192.168.77",
		"IP_SHORT":                  "1",
		"START_LEASE":               "10",
		"END_LEASE":                 "250",
		"LEASE_DURATION":            "12h",
		"LEASE_FILE_LOCATION":       "$PIRATEBOX_FOLDER/tmp/lease.file",
		"NETMASK":                   "255.255.255.0",
		"IPV6_ENABLE":               "no",
		"IPV6_ADVERT":               "radvd",
		"IPV6_IP":                   "fdc0:ffea::1",
		"IPV6_MASK":                 "64",
		"IPV6_PREFIX":               "fdc0:ffea",
		"FIREWALL_FETCH_DNS":        "yes",
		"FIREWALL_FETCH_HTTP":       "no",
		"NODE_CONFIG_ACTIVE":        "no",
		"NODE_NAME":                 "",
		"NODE_GEN":                  "yes",
		"NODE_INTERFACE":            "$INTERFACE",
		"NODE_IPV6_IP":              "",
		"NODE_IPV6_MASK":            "/64",
		"NODE_IPV6_SET_IP":          "no",
		"DROOPY_ENABLED":            "yes",
		"DROOPY_TXT":                "",
		"DROOPY_PORT":               "8080",
		"DROOPY_CHMOD":              "755",
		"DROOPY_USERDIR":            "",
		"DROOPY_USE_USER":           "yes",
		"CHATFILE":                  "$WWW_FOLDER/cgi-bin/data.pso",
		"GEN_CHATFILE":              "$WWW_FOLDER/chat_content.html",
		"GEN_DISKFILE":              "$WWW_FOLDER/diskusage.html",
		"RESET_CHAT":                "yes",
		"SHOUTBOX_ENABLED":          "yes",
		"SHOUTBOX_CLIENT_TIMESTAMP": "no",
		"CUSTOM_DIRLIST_COPY":       "yes",
		"PBX_JSON_GENERATION":       "yes",
		"PBX_JSON_FILE":             "$WWW_FOLDER/piratebox_config.json",
		"GLOBAL_CHAT":               "no",
		"GLOBAL_DEST":               "[ #255.255.255.255# , #10.255.255.255# , #$NET.255# ]",
		"LIGHTTPD_USER":             "nobody",
		"LIGHTTPD_GROUP":            "nogroup",
		"ENABLE_IRC_SERVER":         "no",
		"HTTP_ADDR":                 "",
		"HTTP_PORT":                 "80",
		"UPLOAD_MAX_SIZE_MB":        "5",
	}
}

// Load parses path on top of the built-in defaults and returns a typed
// configuration.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("config: %w (run `luffy setup` or pass --config)", err)
	}
	parser := NewParser(filepath.Dir(path))
	parsed, err := parser.ParseFile(path)
	if err != nil {
		return nil, err
	}
	values := Defaults()
	for k, v := range parsed {
		values[k] = v
	}
	// The mesh node settings live in their own file, loaded by the original
	// piratebox.conf through `. $NODE_CONFIG`. Load it the same way, after
	// the main file so node values win.
	nodePath := values.Get("NODE_CONFIG")
	if nodePath != "" {
		if _, err := os.Stat(nodePath); err == nil {
			nodeParser := NewParser(filepath.Dir(nodePath))
			nodeValues, err := nodeParser.ParseFile(nodePath)
			if err != nil {
				return nil, err
			}
			for k, v := range nodeValues {
				values[k] = v
			}
			parser.Warnings = append(parser.Warnings, nodeParser.Warnings...)
		}
	}
	cfg := FromValues(values)
	cfg.Path = path
	if len(parser.Warnings) > 0 {
		cfg.warnings = parser.Warnings
	}
	return cfg, nil
}

// FromValues builds a typed Config from raw values. Missing keys fall back
// to Defaults().
func FromValues(values Values) *Config {
	base := Defaults()
	merged := Values{}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range values {
		merged[k] = v
	}
	values = resolveReferences(merged)

	cfg := &Config{Values: values}
	cfg.PirateboxFolder = values.Get("PIRATEBOX_FOLDER")
	cfg.WwwFolder = values.Get("WWW_FOLDER")
	cfg.WwwContent = values.Get("WWW_CONTENT")
	cfg.ShareFolder = values.Get("SHARE_FOLDER")
	cfg.UploadFolder = values.Get("UPLOADFOLDER")
	cfg.Timesave = values.Get("TIMESAVE")
	cfg.TimesaveFormat = values.Get("TIMESAVE_FORMAT")
	cfg.NodeConfigPath = values.Get("NODE_CONFIG")
	cfg.VersionFile = filepath.Join(cfg.PirateboxFolder, "version")

	cfg.ChatFile = values.Get("CHATFILE")
	cfg.GenChatFile = values.Get("GEN_CHATFILE")
	cfg.GenDiskFile = values.Get("GEN_DISKFILE")
	cfg.LeaseFile = values.Get("LEASE_FILE_LOCATION")
	cfg.HostsFile = filepath.Join(cfg.PirateboxFolder, "conf", "hosts_generated")
	cfg.HostsMeshFile = filepath.Join(cfg.PirateboxFolder, "conf", "hosts_mesh")
	cfg.DNSMasqConf = filepath.Join(cfg.PirateboxFolder, "conf", "dnsmasq_generated.conf")
	cfg.RadvdConf = filepath.Join(cfg.PirateboxFolder, "conf", "radvd_generated.conf")
	cfg.AvahiSchema = filepath.Join(cfg.PirateboxFolder, "conf", "avahi", "avahi-daemon.conf.schema")
	cfg.AvahiConf = filepath.Join(cfg.PirateboxFolder, "conf", "avahi", "avahi-daemon.conf")
	cfg.InitDone = filepath.Join(cfg.PirateboxFolder, "conf", "init_done")

	cfg.DoIfconfig = values.Yes("DO_IFCONFIG")
	cfg.DoIW = values.Yes("DO_IW")
	cfg.PhyIf = values.Get("PHY_IF")
	cfg.ProbeInterface = values.Yes("PROBE_INTERFACE")
	cfg.ProbeTime = atoiDefault(values.Get("PROBE_TIME"), 10)
	cfg.UseAPN = values.Yes("USE_APN")
	cfg.UseDNSMasq = values.Yes("USE_DNSMASQ")

	cfg.Host = values.Get("HOST")
	cfg.Interface = values.Get("INTERFACE")
	cfg.DNSMasqInterface = values.Get("DNSMASQ_INTERFACE")
	cfg.DoBridge = values.Yes("DO_BRIDGE")
	cfg.Bridge = values.Get("BRIDGE")
	cfg.Net = values.Get("NET")
	cfg.IPShort = values.Get("IP_SHORT")
	cfg.StartLease = values.Get("START_LEASE")
	cfg.EndLease = values.Get("END_LEASE")
	cfg.LeaseDuration = values.Get("LEASE_DURATION")
	cfg.Netmask = values.Get("NETMASK")
	if _, ok := values["IP"]; !ok {
		values["IP"] = cfg.Net + "." + cfg.IPShort
	}

	cfg.IPv6Enable = values.Yes("IPV6_ENABLE")
	cfg.IPv6Advert = values.Get("IPV6_ADVERT")
	cfg.IPv6IP = values.Get("IPV6_IP")
	cfg.IPv6Mask = values.Get("IPV6_MASK")
	cfg.IPv6Prefix = values.Get("IPV6_PREFIX")

	cfg.FirewallFetchDNS = values.Yes("FIREWALL_FETCH_DNS")
	cfg.FirewallFetchHTTP = values.Yes("FIREWALL_FETCH_HTTP")

	cfg.NodeConfigActive = values.Yes("NODE_CONFIG_ACTIVE")
	cfg.NodeName = values.Get("NODE_NAME")
	cfg.NodeGen = values.GetDefault("NODE_GEN", "yes") == "yes"
	cfg.NodeInterface = values.GetDefault("NODE_INTERFACE", cfg.Interface)
	cfg.NodeIPv6IP = values.Get("NODE_IPV6_IP")
	cfg.NodeIPv6Mask = values.Get("NODE_IPV6_MASK")
	cfg.NodeIPv6SetIP = values.Yes("NODE_IPV6_SET_IP")

	cfg.User = values.Get("LIGHTTPD_USER")
	cfg.HTTPAddr = values.Get("HTTP_ADDR")
	cfg.HTTPPort = atoiDefault(values.Get("HTTP_PORT"), 80)
	cfg.UploadMaxBytes = int64(atoiDefault(values.Get("UPLOAD_MAX_SIZE_MB"), 5)) << 20

	cfg.DroopyEnabled = values.Yes("DROOPY_ENABLED")
	cfg.DroopyPort = values.Get("DROOPY_PORT")
	cfg.DroopyTxt = values.Get("DROOPY_TXT")
	cfg.DroopyChmod = values.Get("DROOPY_CHMOD")
	cfg.DroopyUserDir = values.Get("DROOPY_USERDIR")
	cfg.CustomDirlistCopy = values.Yes("CUSTOM_DIRLIST_COPY")
	cfg.PbxJSONGeneration = values.Yes("PBX_JSON_GENERATION")
	cfg.ShoutboxEnabled = values.Yes("SHOUTBOX_ENABLED")
	cfg.ShoutboxClientTime = values.Yes("SHOUTBOX_CLIENT_TIMESTAMP")
	cfg.ResetChat = values.Yes("RESET_CHAT")
	cfg.GlobalChat = values.Yes("GLOBAL_CHAT")
	cfg.GlobalDest = ParseDestinations(values.Get("GLOBAL_DEST"))
	cfg.EnableIRCServer = values.Yes("ENABLE_IRC_SERVER")

	return cfg
}

// IP returns the configured IPv4 address of the box.
func (c *Config) IP() string {
	if ip := c.Values.Get("IP"); ip != "" {
		return ip
	}
	return strings.TrimSuffix(c.Net+"."+c.IPShort, ".")
}

// UploadHTTPPort returns the port of the droopy compatible upload listener.
func (c *Config) UploadHTTPPort() int {
	return atoiDefault(c.DroopyPort, 8080)
}

// NodeHostname returns the optional mesh node hostname, following the
// original node_name_generation.sh rules.
func (c *Config) NodeHostname() (string, bool) {
	if !c.NodeConfigActive {
		return "", false
	}
	name := c.NodeName
	if name == "" {
		return c.Host, c.Host != ""
	}
	if !c.NodeGen {
		return name, true
	}
	return name + "." + c.Host, true
}

// Warnings returns non-fatal parser diagnostics.
func (c *Config) Warnings() []string { return c.warnings }

// Validate returns the errors that would prevent a successful start.
func (c *Config) Validate() []error {
	var errs []error
	require := func(value, name, hint string) {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Errorf("config: %s is empty (%s)", name, hint))
		}
	}
	if c.PirateboxFolder == "" {
		errs = append(errs, fmt.Errorf("config: PIRATEBOX_FOLDER is empty"))
	}
	if c.UseAPN || c.DoIfconfig {
		require(c.Interface, "INTERFACE", "e.g. wlan0")
	}
	if c.DoIfconfig {
		require(c.IP(), "IP/NET/IP_SHORT", "e.g. IP=192.168.77.1")
		require(c.Netmask, "NETMASK", "e.g. 255.255.255.0")
	}
	if c.UseDNSMasq {
		require(c.Net, "NET", "e.g. 192.168.77")
		require(c.StartLease, "START_LEASE", "e.g. 10")
		require(c.EndLease, "END_LEASE", "e.g. 250")
	}
	if c.HTTPPort <= 0 || c.HTTPPort > 65535 {
		errs = append(errs, fmt.Errorf("config: HTTP_PORT %d out of range", c.HTTPPort))
	}
	if c.UploadMaxBytes <= 0 {
		errs = append(errs, fmt.Errorf("config: UPLOAD_MAX_SIZE_MB must be positive"))
	}
	return errs
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}
