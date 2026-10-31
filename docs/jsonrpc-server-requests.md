# App Serverのサーバー起点JSON-RPC request

App Serverは通知だけでなく、承認やユーザー入力のためにclientへJSON-RPC requestを送ります。runner-agentはrequestに含まれる`id`を保持して応答する必要があります。

`JSONRPCClient`は通常の`Call`応答とサーバー起点requestを同じstdinストリームから読み分けます。request handlerが結果を返すと、同じrequest IDを付けたJSON-RPC responseをApp Serverへ書き戻します。未登録のrequestは`-32601`で応答して、App Serverを永遠に待たせません。

承認の判断そのものは次のPRで追加します。このPRはプロトコル上の応答境界だけを固定します。
