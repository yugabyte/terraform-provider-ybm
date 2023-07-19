## Debugging 

This provider support [terraform plugin debugging](https://developer.hashicorp.com/terraform/plugin/debugging) pattern.


## Debugging with VS Code

Please create the following `.vscode/launch.json` into the repository directory 

```json
{
    "version": "0.2.0",
    "configurations": [
    
        {
            "name": "Debug Terraform Provider",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            // this assumes your workspace is the root of the repo
            "program": "./",
            "env": {},
            "args": [
                "-debug",
            ]
        }
    ]
}
```

Then launch ["Run and Debug"](https://code.visualstudio.com/docs/editor/debugging) from VS Code.


When the debugger start you should see the Terraform debugging information

```
Provider started, to attach Terraform set the TF_REATTACH_PROVIDERS env var:

        TF_REATTACH_PROVIDERS='{"registry.terraform.io/my-org/my-provider":{"Protocol":"grpc","Pid":3382870,"Test":true,"Addr":{"Network":"unix","String":"/tmp/plugin713096927"}}}'
```

Please use this environment variable to run your Terraform command.

```shell
TF_REATTACH_PROVIDERS='{"registry.terraform.io/my-org/my-provider":{"Protocol":"grpc","Pid":3382870,"Test":true,"Addr":{"Network":"unix","String":"/tmp/plugin713096927"}}}' terraform plan
```