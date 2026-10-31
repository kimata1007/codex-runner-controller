# App Serverのサーバー起点JSON-RPC request

App Serverは通知だけでなく、承認やユーザー入力のためにclientへJSON-RPC requestを送ります。runner-agentはrequestに含まれる`id`を保持して応答する必要があります。

`JSONRPCClient`は通常の`Call`応答とサーバー起点requestを同じstdinストリームから読み分けます。request handlerが結果を返すと、同じrequest IDを付けたJSON-RPC responseをApp Serverへ書き戻します。未登録のrequestは`-32601`で応答して、App Serverを永遠に待たせません。

承認requestは`ApprovalManager`が保留し、Runner APIへ`approval/requested`イベントを送ります。クライアントの回答はRunner API経由でrunner-agentへ戻り、JSON-RPC responseとしてApp Serverへ返します。
