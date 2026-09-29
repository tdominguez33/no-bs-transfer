export namespace config {
	
	export class AppConfig {
	    useCustomDomain: boolean;
	    customDomain: string;
	    tunnelToken: string;
	    fixedPort: number;
	    savePortable: boolean;
	    windowWidth?: number;
	    windowHeight?: number;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.useCustomDomain = source["useCustomDomain"];
	        this.customDomain = source["customDomain"];
	        this.tunnelToken = source["tunnelToken"];
	        this.fixedPort = source["fixedPort"];
	        this.savePortable = source["savePortable"];
	        this.windowWidth = source["windowWidth"];
	        this.windowHeight = source["windowHeight"];
	    }
	}

}

export namespace main {
	
	export class CloudflaredStatus {
	    installed: boolean;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new CloudflaredStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.path = source["path"];
	    }
	}
	export class FileShareItem {
	    id: string;
	    name: string;
	    size: string;
	    directUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new FileShareItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.directUrl = source["directUrl"];
	    }
	}
	export class MultiShareResult {
	    publicBaseUrl: string;
	    bundleUrl: string;
	    files: FileShareItem[];
	    localUrls: string[];
	    totalSize: string;
	
	    static createFrom(source: any = {}) {
	        return new MultiShareResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.publicBaseUrl = source["publicBaseUrl"];
	        this.bundleUrl = source["bundleUrl"];
	        this.files = this.convertValues(source["files"], FileShareItem);
	        this.localUrls = source["localUrls"];
	        this.totalSize = source["totalSize"];
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

export namespace transfer {
	
	export class SharedFile {
	    id: string;
	    path: string;
	    name: string;
	    size: string;
	    rawBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new SharedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.rawBytes = source["rawBytes"];
	    }
	}

}

