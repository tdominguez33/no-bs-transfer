export namespace config {
	
	export class AppConfig {
	    language?: string;
	    networkMode: string;
	    useCustomDomain: boolean;
	    customDomain: string;
	    tunnelToken: string;
	    fixedPort: number;
	    savePortable: boolean;
	    downloadDir: string;
	    windowWidth?: number;
	    windowHeight?: number;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.networkMode = source["networkMode"];
	        this.useCustomDomain = source["useCustomDomain"];
	        this.customDomain = source["customDomain"];
	        this.tunnelToken = source["tunnelToken"];
	        this.fixedPort = source["fixedPort"];
	        this.savePortable = source["savePortable"];
	        this.downloadDir = source["downloadDir"];
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
	    localUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new FileShareItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.directUrl = source["directUrl"];
	        this.localUrl = source["localUrl"];
	    }
	}
	export class MultiShareResult {
	    publicBaseUrl: string;
	    bundleUrl: string;
	    localBundleUrl: string;
	    files: FileShareItem[];
	    localUrls: string[];
	    totalSize: string;
	    networkMode: string;
	
	    static createFrom(source: any = {}) {
	        return new MultiShareResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.publicBaseUrl = source["publicBaseUrl"];
	        this.bundleUrl = source["bundleUrl"];
	        this.localBundleUrl = source["localBundleUrl"];
	        this.files = this.convertValues(source["files"], FileShareItem);
	        this.localUrls = source["localUrls"];
	        this.totalSize = source["totalSize"];
	        this.networkMode = source["networkMode"];
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
	export class ReceiveResult {
	    publicUrl: string;
	    uploadUrl: string;
	    localUploadUrl: string;
	    localUrls: string[];
	    downloadDir: string;
	    networkMode: string;
	
	    static createFrom(source: any = {}) {
	        return new ReceiveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.publicUrl = source["publicUrl"];
	        this.uploadUrl = source["uploadUrl"];
	        this.localUploadUrl = source["localUploadUrl"];
	        this.localUrls = source["localUrls"];
	        this.downloadDir = source["downloadDir"];
	        this.networkMode = source["networkMode"];
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

