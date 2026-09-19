// Package sysconfig generates the configuration files that the original
// generate_config_files.sh produced:
//
//	conf/hosts_generated
//	conf/dnsmasq_generated.conf
//	conf/radvd_generated.conf
//	conf/lighttpd/env
//	conf/avahi/avahi-daemon.conf
//
// The output is byte compatible with the shell version so dnsmasq, radvd
// and avahi keep working unchanged.
package sysconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"luffy/internal/config"
)

// Generator writes derived configuration files.
type Generator struct {
	Cfg *config.Config
	// ReadFile can be replaced in tests.
	ReadFile func(string) ([]byte, error)
	// WriteFile can be replaced in tests.
	WriteFile func(string, []byte, os.FileMode) error
}

// New returns a generator for cfg.
func New(cfg *config.Config) *Generator {
	return &Generator{
		Cfg:       cfg,
		ReadFile:  os.ReadFile,
		WriteFile: writeFile,
	}
}

// writeFile creates the parent directory before writing.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sysconfig: create %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, data, mode)
}

// GeneratedPaths returns every file Generate writes.
func (g *Generator) GeneratedPaths() []string {
	return []string{
		g.Cfg.HostsFile,
		g.Cfg.DNSMasqConf,
		g.Cfg.RadvdConf,
		filepath.Join(g.Cfg.PirateboxFolder, "conf", "lighttpd", "env"),
		g.Cfg.AvahiConf,
	}
}

// Generate writes all derived files.
func (g *Generator) Generate() error {
	if err := g.GenerateHosts(); err != nil {
		return err
	}
	if err := g.GenerateDNSMasq(); err != nil {
		return err
	}
	if g.Cfg.IPv6Enable && g.Cfg.IPv6Advert == "radvd" {
		if err := g.GenerateRadvd(); err != nil {
			return err
		}
	}
	if err := g.GenerateLighttpdEnv(); err != nil {
		return err
	}
	if err := g.GenerateAvahi(); err != nil {
		return err
	}
	return nil
}

// GenerateHosts recreates conf/hosts_generated from conf/hosts plus the
// box address, including the optional mesh node entry.
func (g *Generator) GenerateHosts() error {
	defaultHosts, err := g.ReadFile(filepath.Join(g.Cfg.PirateboxFolder, "conf", "hosts"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sysconfig: read hosts: %w", err)
	}
	var b strings.Builder
	b.Write(defaultHosts)
	fmt.Fprintf(&b, "%s     %s \n", g.Cfg.IP(), g.Cfg.Host)
	if g.Cfg.IPv6Enable {
		fmt.Fprintf(&b, "%s     %s \n", g.Cfg.IPv6IP, g.Cfg.Host)
	} else {
		fmt.Fprintf(&b, "#     %s \n", g.Cfg.Host)
	}
	if nodeName, ok := g.Cfg.NodeHostname(); ok && g.Cfg.NodeIPv6IP != "" {
		fmt.Fprintf(&b, "%s   %s  \n", g.Cfg.NodeIPv6IP, nodeName)
	}
	return g.WriteFile(g.Cfg.HostsFile, []byte(b.String()), 0o644)
}

// GenerateDNSMasq recreates conf/dnsmasq_generated.conf.
func (g *Generator) GenerateDNSMasq() error {
	base, err := g.ReadFile(filepath.Join(g.Cfg.PirateboxFolder, "conf", "dnsmasq_default.conf"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sysconfig: read dnsmasq default: %w", err)
	}
	var b strings.Builder
	b.Write(base)
	if iface := g.Cfg.DNSMasqInterface; iface != "" {
		fmt.Fprintf(&b, "interface=%s\n", iface)
	}
	fmt.Fprintf(&b, "dhcp-range=%s.%s,%s.%s,%s\n",
		g.Cfg.Net, g.Cfg.StartLease, g.Cfg.Net, g.Cfg.EndLease, g.Cfg.LeaseDuration)
	fmt.Fprintf(&b, "address=/#/%s.%s\n", g.Cfg.Net, g.Cfg.IPShort)
	fmt.Fprintf(&b, "dhcp-leasefile=%s\n", g.Cfg.LeaseFile)
	fmt.Fprintf(&b, "addn-hosts=%s\n", g.Cfg.HostsFile)
	fmt.Fprintf(&b, "addn-hosts=%s\n", g.Cfg.HostsMeshFile)
	if g.Cfg.IPv6Enable && g.Cfg.IPv6Advert == "dnsmasq" {
		b.WriteString("#----- V6 Stuff\n")
		fmt.Fprintf(&b, "dchp-range=%s::, ra-stateless\n", g.Cfg.IPv6Prefix)
	}
	return g.WriteFile(g.Cfg.DNSMasqConf, []byte(b.String()), 0o644)
}

// GenerateRadvd recreates conf/radvd_generated.conf.
func (g *Generator) GenerateRadvd() error {
	content := fmt.Sprintf(`#---- generated file ---
    interface %s {
       AdvSendAdvert on;
       MinRtrAdvInterval 3;
       MaxRtrAdvInterval 10;
       prefix %s::/%s {
           AdvOnLink on; 
	   AdvAutonomous on; 
	   AdvRouterAddr on; 
       };
    };
       `, g.Cfg.DNSMasqInterface, g.Cfg.IPv6Prefix, g.Cfg.IPv6Mask)
	return g.WriteFile(g.Cfg.RadvdConf, []byte(content), 0o644)
}

// GenerateLighttpdEnv recreates conf/lighttpd/env. Luffy does not run
// lighttpd, but the file is kept so a downgrade to the Python release keeps
// working and external tooling does not break.
func (g *Generator) GenerateLighttpdEnv() error {
	broadcastLine := ""
	if g.Cfg.GlobalChat {
		dest := "[" + strings.Join(quoteAll(g.Cfg.GlobalDest), " , ") + "]"
		broadcastLine = fmt.Sprintf("\t   \"SHOUTBOX_BROADCAST_DESTINATIONS\" => \"%s\" , \n", dest)
	}
	env := fmt.Sprintf(`setenv.add-environment = ( 
	   "PYTHONPATH"             => "%s:%s/python_lib", 
	   "SHOUTBOX_GEN_HTMLFILE"  => "%s" , 
	   "SHOUTBOX_CHATFILE"      => "%s" , 
	   "SHOUTBOX_CLIENT_TIMESTAMP" => "%s" , 
	   "UPLOAD_PATH" => "%s" , 
     	   "DISK_GEN_HTMLFILE"      => "%s" , 
%s
        )`, g.Cfg.Values.Get("PIRATEBOX_PYTHONPATH"), g.Cfg.PirateboxFolder,
		g.Cfg.GenChatFile, g.Cfg.ChatFile, yesNo(g.Cfg.ShoutboxClientTime),
		g.Cfg.UploadFolder, g.Cfg.GenDiskFile, broadcastLine)
	return g.WriteFile(filepath.Join(g.Cfg.PirateboxFolder, "conf", "lighttpd", "env"), []byte(env), 0o644)
}

// GenerateAvahi renders conf/avahi/avahi-daemon.conf from the schema,
// replacing the masked hostname with the dot-to-underscore form the
// original used.
func (g *Generator) GenerateAvahi() error {
	schema, err := g.ReadFile(g.Cfg.AvahiSchema)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("sysconfig: read avahi schema: %w", err)
	}
	host := g.Cfg.Host
	if nodeName, ok := g.Cfg.NodeHostname(); ok {
		host = nodeName
	}
	masked := strings.ReplaceAll(host, ".", "_")
	rendered := strings.ReplaceAll(string(schema), "#####MASKED_HOSTNAME#####", masked)
	return g.WriteFile(g.Cfg.AvahiConf, []byte(rendered), 0o644)
}

func quoteAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, `#`+v+`#`)
	}
	return out
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
