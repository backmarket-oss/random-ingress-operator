# random-ingress-operator

A Kubernetes operator to manage randomly named Ingresses with a maximum duration. When that duration is reached, the ingress is updated with a new random name.

## Example

Taking this `RandomIngress` resource:

```yaml
apiVersion: networking.backmarket.io/v1alpha1
kind: RandomIngress
metadata:
  name: example
spec:
  ingressTemplate:
    spec:
      rules:
      # The controller will enforce that a |RANDOM| tag is present in every host, and refuse to create the Ingress otherwise
      - host: "|RANDOM|.example.com" 
        http:
          paths:
          - backend:
              service:
                name: example-service
                port:
                  number: 80
```

The operator will create this Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: exampleingress-123456ab12ef
  annotations:
    networking.backmarket.io/expires-at: 2021-09-02T21:08:08Z 
spec:
  rules:
  - host: "a2e7a42e-3be2-4921-b187-0f067dbd6520.example.com"
    http:
      paths:
      - backend:
          serviceName: example-service
          port:
            number: 80
```

The UUID will be changed periodically (by default every eight hours).

## Keeping the ingresses really hidden

If you expose HTTPS endpoints, you should avoid creating one certificate per ingress, and use a wildcard instead.
Public certificate creation is recorded in Certificate Transparency Logs nowadays, which can be queried by anyone
and can be used by someone to find the names of your random ingresses.

Using a wildcard certificate instead will avoid this exposure.
