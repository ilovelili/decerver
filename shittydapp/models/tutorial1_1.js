var c3d = {
	"contract" : "",

	"CreateFileContract" : function(script,language){
		monk.DeployContract(script,language);
	},

	// filename and data are both strings.
	"CreateFile" : function(filename, data){
		var hash = ipfs.PushBlock(data);
		var txData = [];
		txData.push(hash[0,32]);
		txData.push(hash[32,34]);
		monk.Msg(this.contract, txData);
		return;
	},

	"GetFile" : function(filename){
		var filehash1 = monk.StorageAt(this.contract,filename);
 		var filehash2 = monk.StorageAt(this.contract,Add(filename),1);
 		var filehash = filehash1 + filehash2[2,4];
		var data = ipfs.GetBlock(filehash);
		return data;
	},
}

function receive(request){
	// We only have one action model (c3d), so we know that commands has only
	// one value in it, and that's the name of the action.
	var action = request.commands[0];
	switch (action) {
		case "CreateFile":
			// The filename and data is extracted from the parameters.
			var filename = request.params["filename"];
			var data = request.params["data"];
			// Run function, and return the value.
			return c3d.CreateFile(filename,data);
		case "GetFile":
			var filename = request.params["filename"];
			return c3d.GetFile(filename);
		case "CreateFileContract":
			var data = request.params["script"];
			var language = request.params["language"];
			return c3d.CreateFileContract(data,language);
		default:
			// If there are problems with the in-data, just return null.
			return null;
	}
};