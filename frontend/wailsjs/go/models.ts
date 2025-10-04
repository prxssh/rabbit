export namespace torrent {
	
	export class File {
	    Length: number;
	    Path: string[];
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Length = source["Length"];
	        this.Path = source["Path"];
	    }
	}
	export class Info {
	    Hash: number[];
	    Name: string;
	    PieceLength: number;
	    Pieces: number[][];
	    Private: boolean;
	    Length: number;
	    Files: File[];
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Hash = source["Hash"];
	        this.Name = source["Name"];
	        this.PieceLength = source["PieceLength"];
	        this.Pieces = source["Pieces"];
	        this.Private = source["Private"];
	        this.Length = source["Length"];
	        this.Files = this.convertValues(source["Files"], File);
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
	    Info?: Info;
	    Announce: string;
	    AnnounceList: string[][];
	    // Go type: time
	    CreationDate: any;
	    CreatedBy: string;
	    Comment: string;
	    Encoding: string;
	    URLs: string[];
	
	    static createFrom(source: any = {}) {
	        return new Metainfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Info = this.convertValues(source["Info"], Info);
	        this.Announce = source["Announce"];
	        this.AnnounceList = source["AnnounceList"];
	        this.CreationDate = this.convertValues(source["CreationDate"], null);
	        this.CreatedBy = source["CreatedBy"];
	        this.Comment = source["Comment"];
	        this.Encoding = source["Encoding"];
	        this.URLs = source["URLs"];
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
	    Size: number;
	    ClientID: number[];
	    Metainfo?: Metainfo;
	    // Go type: tracker
	    Tracker?: any;
	    // Go type: peer
	    PeerManager?: any;
	
	    static createFrom(source: any = {}) {
	        return new Torrent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Size = source["Size"];
	        this.ClientID = source["ClientID"];
	        this.Metainfo = this.convertValues(source["Metainfo"], Metainfo);
	        this.Tracker = this.convertValues(source["Tracker"], null);
	        this.PeerManager = this.convertValues(source["PeerManager"], null);
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

