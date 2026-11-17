# connect-slicer-bridge

Send prints from PrusaSlicer 2.x (and other slicers that speak the OctoPrint
upload API) to a self-hosted [Prusa Connect](https://github.com/prusa3d/prusa-connect)
server.

PrusaSlicer 2.x can't upload to the local Prusa Connect server: its "Prusa Connect"
host type only talks to Prusa's cloud
([prusa3d/prusa-connect#7](https://github.com/prusa3d/prusa-connect/issues/7)).
This bridge pretends to be OctoPrint. It takes the upload, stores it in Connect,
and has Connect send it to the printer and, if asked, start the print.

```
[PrusaSlicer] > OctoPrint API > [bridge] > REST API > [local Prusa Connect] > [printer]
```

The bridge stores nothing and holds no secrets. The API key you enter in the
slicer is a Prusa Connect API credential, and the bridge passes it through.
Connect decides what that key is allowed to do.

This project is not affiliated with or endorsed by Prusa Research.

## Compatibility

| | |
|---|---|
| Prusa Connect | 0.1.0-beta.1 |
| PrusaSlicer | 2.9.x, host type OctoPrint |

Prusa Connect is in beta and its API can change between releases.

## Run it

```yaml
services:
  connect-slicer-bridge:
    image: ghcr.io/bada55fa7a55/connect-slicer-bridge:latest
    restart: unless-stopped
    ports:
      - "17481:17481"
    environment:
      CONNECT_URL: http://<connect host>:8080
```

Or run the binary: `connect-slicer-bridge -connect http://<connect host>:8080`.

| flag | env | default | |
|---|---|---|---|
| `-connect` | `CONNECT_URL` | | Base URL of the Prusa Connect server. Required |
| `-listen` | `LISTEN_ADDR` | `:17481` | Address to listen on |
| `-storage` | `CONNECT_STORAGE` | `local` | Connect storage that receives uploads |
| `-printer-storage` | `PRINTER_STORAGE` | `usb` | Storage on the printer that files go to |
| `-keep` | `KEEP_FOLDER` | | Keep uploads in this folder of the Connect storage. Without it, uploads are temporary copies that Connect removes |

## Set up PrusaSlicer

1. In Prusa Connect, open **Settings → API credentials → New credential**
   and choose the scope **operate**. Copy the token.
2. In PrusaSlicer, add a physical printer:
   - **Host type:** OctoPrint
   - **Hostname, IP or URL:** `http://<bridge host>:17481/<printer uuid>`
   - **API Key / Password:** the token
3. Click **Test**.

To find the printer UUID, open the printer in Prusa Connect. It's the last
part of the address in the browser,
`http://<connect host>:8080/printers/<printer uuid>/…`, and it's also listed
as **Printer UUID** on the printer's **Printer** tab. With a wrong UUID,
**Test** fails and lists every printer the key can see, with its UUID.
Add one physical printer per printer.

**Upload** puts the file on the printer's USB drive. **Upload and Print**
also starts it. Uploading a file whose name is already on the drive replaces
the old one. PrusaSlicer's upload queue shows Connect's error if the printer
refuses, for example because it is already printing.

## Security

The bridge speaks plain HTTP, just like Prusa Connect's default setup. Run
it only on a network you trust. The token travels in the `X-Api-Key` header.

## License

MIT
