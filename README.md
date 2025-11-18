# renderer-helm

Helm chart renderer with OCI support

Part of the [k8s-manifest-kit](https://github.com/k8s-manifest-kit) organization.

## Installation

```bash
go get github.com/k8s-manifest-kit/renderer-helm
```

## Examples

### Basic Usage

Render a Helm chart from an OCI registry:

```go
package main

import (
    "context"
    
    helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
)

func main() {
    ctx := context.Background()
    
    // Create engine with a single Helm chart
    engine, err := helm.NewEngine(helm.Source{
        Chart:       "oci://registry-1.docker.io/bitnamicharts/nginx",
        ReleaseName: "my-nginx",
    })
    if err != nil {
        panic(err)
    }
    
    // Render the chart
    objects, err := engine.Render(ctx, nil)
    if err != nil {
        panic(err)
    }
    
    // Use the rendered Kubernetes objects
    for _, obj := range objects {
        // Process object...
    }
}
```

### With Values

Override chart values during rendering:

```go
engine, err := helm.NewEngine(helm.Source{
    Chart:       "oci://registry-1.docker.io/bitnamicharts/nginx",
    ReleaseName: "my-nginx",
    Values: helm.Values(map[string]any{
        "replicaCount": 3,
        "image": map[string]any{
            "tag": "1.25",
        },
    }),
})
if err != nil {
    panic(err)
}

objects, err := engine.Render(ctx, map[string]any{
    "service": map[string]any{
        "type": "LoadBalancer",
    },
})
```

### Multiple Charts

Render multiple charts using the renderer directly:

```go
renderer, err := helm.New([]helm.Source{
    {
        Chart:       "oci://registry/chart1",
        ReleaseName: "app",
    },
    {
        Chart:       "oci://registry/chart2",
        ReleaseName: "db",
    },
})
if err != nil {
    panic(err)
}

objects, err := renderer.Process(ctx, nil)
if err != nil {
    panic(err)
}
```

## Documentation

See the main [docs repository](https://github.com/k8s-manifest-kit/docs) for comprehensive documentation.

## Contributing

Contributions are welcome! Please see our [contributing guidelines](https://github.com/k8s-manifest-kit/docs/blob/main/CONTRIBUTING.md).

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
