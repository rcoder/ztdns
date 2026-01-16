# ztdns: CoreDNS plugin to resolve hostnames via ZeroTier Central network configuration

**DANGER**

Very early days, will probably eat your leftovers and leave the containers on the counter, if not worse.

## Usage

- Configure a ZeroTier network and add one or more named hosts
- Add `ZT_API_TOKEN` and `ZT_NETWORK_ID` to your environment. (E.g., use `direnv`)
- `make clean all`
- `make run`
- `dig @localhost -p 1053 <hostname>.<domain>`
- Update Central config to use the server IP + domain

## TODO

- Push DNS config to Central, rather than requiring it to be configured manually
- Decide if it's one network/domain per process, or per config block
