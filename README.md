# bgpaf

Automatic filter generator that consumes IRR data, validates peers against
PeeringDB and produces ready-to-apply configuration snippets for multiple
network operating systems. The tool can still update a VyOS router directly via
the HTTP API, but now also renders Jinja templates for Junos, Cisco IOS/IOS-XE,
Arista EOS and MikroTik RouterOS.

## Features

- Query IRR databases for IPv4 and IPv6 prefixes.
- Look up PeeringDB to retrieve recommended maximum prefix counts per ASN.
- Push prefix-lists to a VyOS instance through its REST API.
- Render vendor-specific Jinja templates that include prefix-lists and
  PeeringDB-based prefix limits.
- Write rendered snippets to disk or print them to stdout for easy automation.

## Configuration

All settings live in `config.yaml`.

```yaml
irrdb: "rr.ntt.net:43"
mode: "vyos"
output_dir: "./output"
template_dir: "./templates"
peeringdb_url: "https://www.peeringdb.com/api"
# The following options are used when mode is "vyos".
host: 1.2.3.4
port: 1234
apikey: "mysecretkey"
# Vendors define which configuration snippets are rendered. Leave empty to use the mode value (if not vyos).
vendors:
  - juniper
  - cisco
  - mikrotik
  - arista
peers:
  - name: "Example Peer"
    asn: 49271
    as_set: "AS49271"
    neighbor_ipv4: "203.0.113.2"
    neighbor_ipv6: "2001:db8::2"
  - name: "Bake Network"
    asn: 12345
    as_set: "AS-BAKE"
    neighbor_ipv4: "198.51.100.2"
    vendors:
      - juniper
      - arista
```

### Key fields

- `mode` &mdash; set to `vyos` to push filters to a VyOS instance. Any other value
  is treated as a vendor name and used when `vendors` is empty.
- `vendors` &mdash; list of vendors to render for every peer. Supported values are
  `juniper`, `cisco`, `mikrotik`, and `arista`.
- `output_dir` &mdash; directory for rendered templates. Leave empty to print to
  stdout.
- `template_dir` &mdash; folder containing the Jinja templates. Defaults to
  `./templates` if omitted.
- `peers` &mdash; each peer requires an `asn` (for PeeringDB), and optionally an
  IRR `as_set`, per-family neighbor addresses, and a per-peer vendor override.
  When `as_set` is omitted the peer `name` is used.

## Running

Build and run the tool as usual:

```bash
go build ./...
./bgpaf
```

The binary will fetch IRR data for every peer, query PeeringDB for prefix
limits, and depending on configuration either push updates to VyOS or render
per-vendor configuration files into `output_dir`.

Rendered files are named `<peer>_<vendor>.conf` and use the Jinja templates from
`template_dir`. If `output_dir` is empty the snippets are printed to stdout with
clear separators so they can be captured by automation pipelines.

## Templates

The repository ships with four templates in `templates/`:

- `juniper.j2`
- `cisco.j2`
- `mikrotik.j2`
- `arista.j2`

They include prefix-list definitions and neighbor configuration examples with
PeeringDB-derived prefix limits. You can customise these templates or add new
ones; as long as they follow basic Jinja syntax (variables, `{% if %}` and
`{% for %}` blocks) they will render correctly.
