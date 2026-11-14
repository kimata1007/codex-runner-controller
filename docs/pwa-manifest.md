# PWA配信マニフェスト

`config/web/`はPWA専用nginxイメージをDeploymentとして配信します。nginxは同一originの`/v1/`を`runner-api` Serviceへプロキシするため、PWAのfetchとBearer認証、runイベントのストリーミングを同じoriginで扱えます。

ServiceはClusterIPです。インターネット公開用のIngressやLoadBalancerは含めていません。スマホから利用する場合は、既存のTailscale内リバースプロキシ、Tailscale OperatorのService公開、または管理者が用意したTailscale到達経路をこのServiceへ接続します。

```sh
kubectl apply -k config/
kubectl -n codex-system port-forward svc/codex-runner-pwa 8080:8080
```

本番ではport-forwardではなく、TailscaleのDNS名を持つ内部プロキシを置いてください。PWAのServiceを直接NodePortにする必要はありません。
