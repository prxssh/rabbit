export namespace config {
    export class Config {
        DefaultDownloadDir: string
        ClientID: number[]
        ReadTimeout: number
        WriteTimeout: number
        DialTimeout: number
        MaxPeers: number
        NumWant: number
        AnnounceInterval: number
        MinAnnounceInterval: number
        MaxAnnounceBackoff: number
        Port: number
        MaxUploadRate: number
        MaxDownloadRate: number
        RateLimitRefresh: number
        PeerOutboundQueueBacklog: number
        PieceDownloadStrategy: number
        MaxInflightRequestsPerPeer: number
        MinInflightRequestsPerPeer: number
        RequestQueueTime: number
        RequestTimeout: number
        EndgameDupPerBlock: number
        EndgameThreshold: number
        MaxRequestsPerPiece: number
        UploadSlots: number
        RechokeInterval: number
        OptimisticUnchokeInterval: number
        PeerHeartbeatInterval: number
        PeerInactivityDuration: number
        KeepAliveInterval: number
        MetricsEnabled: boolean
        MetricsBindAddr: string
        EnableIPv6: boolean
        EnableDHT: boolean
        EnablePEX: boolean
        HasIPV6: boolean

        static createFrom(source: any = {}) {
            return new Config(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.DefaultDownloadDir = source['DefaultDownloadDir']
            this.ClientID = source['ClientID']
            this.ReadTimeout = source['ReadTimeout']
            this.WriteTimeout = source['WriteTimeout']
            this.DialTimeout = source['DialTimeout']
            this.MaxPeers = source['MaxPeers']
            this.NumWant = source['NumWant']
            this.AnnounceInterval = source['AnnounceInterval']
            this.MinAnnounceInterval = source['MinAnnounceInterval']
            this.MaxAnnounceBackoff = source['MaxAnnounceBackoff']
            this.Port = source['Port']
            this.MaxUploadRate = source['MaxUploadRate']
            this.MaxDownloadRate = source['MaxDownloadRate']
            this.RateLimitRefresh = source['RateLimitRefresh']
            this.PeerOutboundQueueBacklog = source['PeerOutboundQueueBacklog']
            this.PieceDownloadStrategy = source['PieceDownloadStrategy']
            this.MaxInflightRequestsPerPeer = source['MaxInflightRequestsPerPeer']
            this.MinInflightRequestsPerPeer = source['MinInflightRequestsPerPeer']
            this.RequestQueueTime = source['RequestQueueTime']
            this.RequestTimeout = source['RequestTimeout']
            this.EndgameDupPerBlock = source['EndgameDupPerBlock']
            this.EndgameThreshold = source['EndgameThreshold']
            this.MaxRequestsPerPiece = source['MaxRequestsPerPiece']
            this.UploadSlots = source['UploadSlots']
            this.RechokeInterval = source['RechokeInterval']
            this.OptimisticUnchokeInterval = source['OptimisticUnchokeInterval']
            this.PeerHeartbeatInterval = source['PeerHeartbeatInterval']
            this.PeerInactivityDuration = source['PeerInactivityDuration']
            this.KeepAliveInterval = source['KeepAliveInterval']
            this.MetricsEnabled = source['MetricsEnabled']
            this.MetricsBindAddr = source['MetricsBindAddr']
            this.EnableIPv6 = source['EnableIPv6']
            this.EnableDHT = source['EnableDHT']
            this.EnablePEX = source['EnablePEX']
            this.HasIPV6 = source['HasIPV6']
        }
    }
}

export namespace meta {
    export class File {
        length: number
        path: string[]

        static createFrom(source: any = {}) {
            return new File(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.length = source['length']
            this.path = source['path']
        }
    }
    export class Info {
        name: string
        pieceLength: number
        pieces: number[][]
        private: boolean
        length: number
        files: File[]

        static createFrom(source: any = {}) {
            return new Info(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.name = source['name']
            this.pieceLength = source['pieceLength']
            this.pieces = source['pieces']
            this.private = source['private']
            this.length = source['length']
            this.files = this.convertValues(source['files'], File)
        }

        convertValues(a: any, classs: any, asMap: boolean = false): any {
            if (!a) {
                return a
            }
            if (a.slice && a.map) {
                return (a as any[]).map((elem) => this.convertValues(elem, classs))
            } else if ('object' === typeof a) {
                if (asMap) {
                    for (const key of Object.keys(a)) {
                        a[key] = new classs(a[key])
                    }
                    return a
                }
                return new classs(a)
            }
            return a
        }
    }
    export class Metainfo {
        info?: Info
        announce: string
        announceList: string[][]
        // Go type: time
        creationDate: any
        createdBy: string
        comment: string
        encoding: string
        urls: string[]
        hash: number[]

        static createFrom(source: any = {}) {
            return new Metainfo(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.info = this.convertValues(source['info'], Info)
            this.announce = source['announce']
            this.announceList = source['announceList']
            this.creationDate = this.convertValues(source['creationDate'], null)
            this.createdBy = source['createdBy']
            this.comment = source['comment']
            this.encoding = source['encoding']
            this.urls = source['urls']
            this.hash = source['hash']
        }

        convertValues(a: any, classs: any, asMap: boolean = false): any {
            if (!a) {
                return a
            }
            if (a.slice && a.map) {
                return (a as any[]).map((elem) => this.convertValues(elem, classs))
            } else if ('object' === typeof a) {
                if (asMap) {
                    for (const key of Object.keys(a)) {
                        a[key] = new classs(a[key])
                    }
                    return a
                }
                return new classs(a)
            }
            return a
        }
    }
}

export namespace peer {
    export class PeerMetrics {
        // Go type: netip
        Addr: any
        Downloaded: number
        Uploaded: number
        RequestsSent: number
        BlocksReceived: number
        BlocksFailed: number
        // Go type: time
        LastActive: any
        // Go type: time
        ConnectedAt: any
        ConnectedFor: number
        DownloadRate: number
        UploadRate: number
        IsChoked: boolean
        IsInterested: boolean

        static createFrom(source: any = {}) {
            return new PeerMetrics(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.Addr = this.convertValues(source['Addr'], null)
            this.Downloaded = source['Downloaded']
            this.Uploaded = source['Uploaded']
            this.RequestsSent = source['RequestsSent']
            this.BlocksReceived = source['BlocksReceived']
            this.BlocksFailed = source['BlocksFailed']
            this.LastActive = this.convertValues(source['LastActive'], null)
            this.ConnectedAt = this.convertValues(source['ConnectedAt'], null)
            this.ConnectedFor = source['ConnectedFor']
            this.DownloadRate = source['DownloadRate']
            this.UploadRate = source['UploadRate']
            this.IsChoked = source['IsChoked']
            this.IsInterested = source['IsInterested']
        }

        convertValues(a: any, classs: any, asMap: boolean = false): any {
            if (!a) {
                return a
            }
            if (a.slice && a.map) {
                return (a as any[]).map((elem) => this.convertValues(elem, classs))
            } else if ('object' === typeof a) {
                if (asMap) {
                    for (const key of Object.keys(a)) {
                        a[key] = new classs(a[key])
                    }
                    return a
                }
                return new classs(a)
            }
            return a
        }
    }
}

export namespace torrent {
    export class Stats {
        totalPeers: number
        connectingPeers: number
        failedConnection: number
        unchokedPeers: number
        interestedPeers: number
        uploadingTo: number
        downloadingFrom: number
        totalDownloaded: number
        totalUploaded: number
        downloadRate: number
        uploadRate: number
        totalAnnounces: number
        successfulAnnounces: number
        failedAnnounces: number
        totalPeersReceived: number
        currentSeeders: number
        currentLeechers: number
        // Go type: time
        lastAnnounce: any
        // Go type: time
        lastSuccess: any
        progress: number
        peers: peer.PeerMetrics[]
        pieceStates: number[]

        static createFrom(source: any = {}) {
            return new Stats(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.totalPeers = source['totalPeers']
            this.connectingPeers = source['connectingPeers']
            this.failedConnection = source['failedConnection']
            this.unchokedPeers = source['unchokedPeers']
            this.interestedPeers = source['interestedPeers']
            this.uploadingTo = source['uploadingTo']
            this.downloadingFrom = source['downloadingFrom']
            this.totalDownloaded = source['totalDownloaded']
            this.totalUploaded = source['totalUploaded']
            this.downloadRate = source['downloadRate']
            this.uploadRate = source['uploadRate']
            this.totalAnnounces = source['totalAnnounces']
            this.successfulAnnounces = source['successfulAnnounces']
            this.failedAnnounces = source['failedAnnounces']
            this.totalPeersReceived = source['totalPeersReceived']
            this.currentSeeders = source['currentSeeders']
            this.currentLeechers = source['currentLeechers']
            this.lastAnnounce = this.convertValues(source['lastAnnounce'], null)
            this.lastSuccess = this.convertValues(source['lastSuccess'], null)
            this.progress = source['progress']
            this.peers = this.convertValues(source['peers'], peer.PeerMetrics)
            this.pieceStates = source['pieceStates']
        }

        convertValues(a: any, classs: any, asMap: boolean = false): any {
            if (!a) {
                return a
            }
            if (a.slice && a.map) {
                return (a as any[]).map((elem) => this.convertValues(elem, classs))
            } else if ('object' === typeof a) {
                if (asMap) {
                    for (const key of Object.keys(a)) {
                        a[key] = new classs(a[key])
                    }
                    return a
                }
                return new classs(a)
            }
            return a
        }
    }
    export class Torrent {
        size: number
        metainfo?: meta.Metainfo

        static createFrom(source: any = {}) {
            return new Torrent(source)
        }

        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source)
            this.size = source['size']
            this.metainfo = this.convertValues(source['metainfo'], meta.Metainfo)
        }

        convertValues(a: any, classs: any, asMap: boolean = false): any {
            if (!a) {
                return a
            }
            if (a.slice && a.map) {
                return (a as any[]).map((elem) => this.convertValues(elem, classs))
            } else if ('object' === typeof a) {
                if (asMap) {
                    for (const key of Object.keys(a)) {
                        a[key] = new classs(a[key])
                    }
                    return a
                }
                return new classs(a)
            }
            return a
        }
    }
}
