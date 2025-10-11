# Rabbit

<p align="center">
  <video src="https://github.com/user-attachments/assets/046a1b51-4a29-415f-ab8d-6d249c104983" controls autoplay loop muted width="800">
  </video>
</p>

A cross platform BitTorrent client & search engine built with Go and Wails
(Svelte + Typescript).

I built this because I was getting real tired of throwing money at yet another
subscription service. So naturally, the obvious solution was to spend countless
hours writing my own client so that I can pirate for free. Makes total sense,
right?

Jokes aside, what got me hooked was [this random video](https://www.youtube.com/watch?v=cvQrNoCwxgE) 
from 11 years ago which talked about crawling DHTs.

> [!WARNING]
> This project is highly unstable and under active development. Expect breaking
> changes and incomplete features.

## Features

- Torrent metadata parsing.
- HTTP and UDP Tracker Protocol.
- Peer Wire Protocol.
- Piece download and verification.
- Sequential, random, and rarest-first piece selection and download strategy.
- Cross-platform desktop UI using [Wails](https://wails.io/).

## Build

Make sure you've [Wails](https://wails.io/) and [Go](https://go.dev/dl/)
(version >= 1.25) installed on your system.

To run the project in Wails dev mode:

```bash

git clone https://github.com/prxssh/rabbit
cd rabbit
wails dev
```

## References

- [BitTorrent Specification](https://www.bittorrent.org/beps/bep_0000.html)
- [Kademlia DHT](https://codethechange.stanford.edu/guides/guide_kademlia.html)
- [Arpit Bhayani BitTorrent Internals Playlist](https://www.youtube.com/watch?v=v7cR0ZolaUA&list=PLsdq-3Z1EPT1rNeq2GXpnivaWINnOaCd0)
- [Arpit Bhayani Kademlia DHT](https://www.youtube.com/watch?v=_kCHOpINA5g)
- [Writing a Fast Piece Picker](https://blog.libtorrent.org/2011/11/writing-a-fast-piece-picker/)
