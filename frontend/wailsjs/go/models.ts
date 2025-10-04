export namespace peer {
	
	export class PeerStats {
	    // Go type: netip
	    Addr: any;
	    Downloaded: number;
	    Uploaded: number;
	    RequestsSent: number;
	    BlocksReceived: number;
	    BlocksFailed: number;
	    // Go type: time
	    LastActive: any;
	    // Go type: time
	    ConnectedAt: any;
	    ConnectedFor: number;
	    DownloadRate: number;
	    IsChoked: boolean;
	    IsInterested: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PeerStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Addr = this.convertValues(source["Addr"], null);
	        this.Downloaded = source["Downloaded"];
	        this.Uploaded = source["Uploaded"];
	        this.RequestsSent = source["RequestsSent"];
	        this.BlocksReceived = source["BlocksReceived"];
	        this.BlocksFailed = source["BlocksFailed"];
	        this.LastActive = this.convertValues(source["LastActive"], null);
	        this.ConnectedAt = this.convertValues(source["ConnectedAt"], null);
	        this.ConnectedFor = source["ConnectedFor"];
	        this.DownloadRate = source["DownloadRate"];
	        this.IsChoked = source["IsChoked"];
	        this.IsInterested = source["IsInterested"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace torrent {
	
	export class File {
	    length: number;
	    path: string[];
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.length = source["length"];
	        this.path = source["path"];
	    }
	}
	export class Info {
	    hash: number[];
	    name: string;
	    pieceLength: number;
	    pieces: number[][];
	    private: boolean;
	    length: number;
	    files: File[];
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.name = source["name"];
	        this.pieceLength = source["pieceLength"];
	        this.pieces = source["pieces"];
	        this.private = source["private"];
	        this.length = source["length"];
	        this.files = this.convertValues(source["files"], File);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Metainfo {
	    info?: Info;
	    announce: string;
	    announceList: string[][];
	    // Go type: time
	    creationDate: any;
	    createdBy: string;
	    comment: string;
	    encoding: string;
	    urls: string[];
	
	    static createFrom(source: any = {}) {
	        return new Metainfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.info = this.convertValues(source["info"], Info);
	        this.announce = source["announce"];
	        this.announceList = source["announceList"];
	        this.creationDate = this.convertValues(source["creationDate"], null);
	        this.createdBy = source["createdBy"];
	        this.comment = source["comment"];
	        this.encoding = source["encoding"];
	        this.urls = source["urls"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Stats {
	    downloaded: number;
	    uploaded: number;
	    downloadRate: number;
	    uploadRate: number;
	    progress: number;
	    peers: peer.PeerStats[];
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloaded = source["downloaded"];
	        this.uploaded = source["uploaded"];
	        this.downloadRate = source["downloadRate"];
	        this.uploadRate = source["uploadRate"];
	        this.progress = source["progress"];
	        this.peers = this.convertValues(source["peers"], peer.PeerStats);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Torrent {
	    size: number;
	    clientId: number[];
	    metainfo?: Metainfo;
	
	    static createFrom(source: any = {}) {
	        return new Torrent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.size = source["size"];
	        this.clientId = source["clientId"];
	        this.metainfo = this.convertValues(source["metainfo"], Metainfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

