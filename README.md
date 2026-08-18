# ssh-hosts

OpenSSHのクライアント設定から、具体的なホストと接続先を一覧表示する小さなCLIです。
`Include` で分割された設定も再帰的に読み込みます。

## インストール

```console
go install github.com/zaubermaerchen/ssh-hosts@latest
```

または、このリポジトリでビルドします。

```console
go build .
```

対話選択を使用する場合は、[`fzf`](https://github.com/junegunn/fzf)、
[`skim`](https://github.com/skim-rs/skim)（実行名 `sk`）、
または [`peco`](https://github.com/peco/peco) のいずれかがPATH上に必要です。

## 使い方

引数を省略すると `~/.ssh/config` を読み込みます。
使える出力・選択オプションは `--json`、`--details`、`--fzf`、`--finder=NAME` です。

```console
$ ssh-hosts
production
staging
development
```

別の設定ファイルを起点にすることもできます。

```console
$ ssh-hosts ./testdata/ssh_config
```

通常の一覧はHostエイリアスだけを1行ずつ表示します。接続先の詳細も表示する場合は
`--details` を指定します。

```console
$ ssh-hosts --details
production	deploy@prod.example.com:22
staging	ubuntu@10.0.0.12:2222
development	developer@development:22
```

詳細は `Hostエイリアス<TAB>ユーザー名@HostName:ポート` 形式です。`User`、`HostName`、
`Port` がない場合は、それぞれ現在のローカルユーザー、Hostエイリアス、`22` を使用します。
IPv6アドレスは `root@[2001:db8::10]:22` のように表示します。

`--fzf` を指定すると、接続先を含む一覧をfuzzy finderで絞り込み、選択したHostエイリアスを表示します。
利用可能なツールを `fzf`、`sk`、`peco` の順で自動検出します。

```console
$ ssh-hosts --fzf
production
$ ssh "$(ssh-hosts --fzf)"
```

`--details` と組み合わせると、選択後も `Hostエイリアス<TAB>ユーザー名@HostName:ポート` 形式で出力します。

```console
$ ssh-hosts --details --fzf
production	deploy@prod.example.com:22
```

使用するツールは `--finder` で明示できます。このオプションだけでも対話選択が有効になります。

```console
$ ssh-hosts --finder=fzf
$ ssh-hosts --finder=sk       # --finder=skim も可
$ ssh-hosts --finder=peco
```

EscまたはCtrl-Cで選択をキャンセルした場合は、finderの非0終了コードをそのまま返します。

### JSON出力

`--json` を指定すると、接続情報を整形済みのJSON配列で出力します。

```console
$ ssh-hosts --json
[
  {
    "alias": "production",
    "user": "deploy",
    "hostname": "prod.example.com",
    "port": 2222,
    "destination": "deploy@prod.example.com:2222"
  }
]
$ ssh-hosts --json | jq -r '.[] | select(.port != 22) | .alias'
production
```

ホストがない場合は `[]` を出力します。`--json` は `--details`、`--fzf`、`--finder` と併用できません。
`--details` は `--fzf` / `--finder` と組み合わせて、finderの候補表示と選択後の出力を詳細形式にできます。

次のような設定では `production` と `staging` だけが表示されます。

```sshconfig
Host production staging *.example.com host? !excluded
Include conf.d/*.conf
```

## 挙動

- `*`、`?`、`[` を含むHostパターンと、`!` から始まる否定パターンを除外します。
- 同じホスト名は最初の1回だけ、設定に現れた順で表示します。
- `User`、`HostName`、`Port` はOpenSSHと同様に、最初に得られた値を採用します。
- `HostName` の `%h` と `User` の対応トークン・環境変数を展開します。
- `Host` / `Match` ブロック内を含むすべての `Include` を探索します。
- Includeの複数パス、glob、`${VAR}`、`~`、OpenSSHの静的な `%` トークンを扱います。
- ユーザー設定の相対Includeパスは、OpenSSHと同じく `~/.ssh` を基準にします。
- globに一致するファイルがない場合は無視します。明示したファイルを読めない場合はエラーになります。
- 接続先がなければ展開できない `%h` などのトークンはエラーになります。

このツールは静的な一覧を生成するため、`Match all` 以外の `Match` 条件や接続時の
CanonicalizeHostnameは評価しません。特に `Match exec` の外部コマンドは実行しません。
