export namespace main {
	
	export class Row {
	    index: number;
	    kind: string;
	    mac: string;
	    ip: string;
	    column: number;
	    row: number;
	    label: string;
	    time: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new Row(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.kind = source["kind"];
	        this.mac = source["mac"];
	        this.ip = source["ip"];
	        this.column = source["column"];
	        this.row = source["row"];
	        this.label = source["label"];
	        this.time = source["time"];
	        this.note = source["note"];
	    }
	}
	export class State {
	    active: boolean;
	    hasSession: boolean;
	    can: string;
	    rack: number;
	    rows: number;
	    columns: number;
	    positions: number;
	    entries: Row[];
	    nextLabel: string;
	    nextColumn: number;
	    nextRow: number;
	    recorded: number;
	    exported: string;
	    version: string;
	    updateState: string;
	    latestVersion: string;
	    latestNotes: string;
	    copied: string;
	    full: boolean;
	    canUndo: boolean;
	    canRedo: boolean;
	    listening: boolean;
	    boundPorts: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.hasSession = source["hasSession"];
	        this.can = source["can"];
	        this.rack = source["rack"];
	        this.rows = source["rows"];
	        this.columns = source["columns"];
	        this.positions = source["positions"];
	        this.entries = this.convertValues(source["entries"], Row);
	        this.nextLabel = source["nextLabel"];
	        this.nextColumn = source["nextColumn"];
	        this.nextRow = source["nextRow"];
	        this.recorded = source["recorded"];
	        this.exported = source["exported"];
	        this.version = source["version"];
	        this.updateState = source["updateState"];
	        this.latestVersion = source["latestVersion"];
	        this.latestNotes = source["latestNotes"];
	        this.copied = source["copied"];
	        this.full = source["full"];
	        this.canUndo = source["canUndo"];
	        this.canRedo = source["canRedo"];
	        this.listening = source["listening"];
	        this.boundPorts = source["boundPorts"];
	        this.error = source["error"];
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

export namespace site {
	
	export class Can {
	    name: string;
	    rows: number;
	    columns: number;
	
	    static createFrom(source: any = {}) {
	        return new Can(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.rows = source["rows"];
	        this.columns = source["columns"];
	    }
	}

}

