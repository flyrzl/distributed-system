# 实验一: Tiny HTTP
## 实验目的
### 分布式系统知识
TODO

## HTTP/1.1协议的极简版
* HTTP request/response的报文格式和HTTP/1.1一致
* 只支持GET和POST方法

### 限制 (HTTP/1.1的子集）
* HTTP连接都是keep-alive语义，即建立TCP链接可以被后续的HTTP请求重用。
* Request-Line中的Request-URI必须是绝对路径。例如"/add", "http://localhost:8080/add"。
* GET/POST的Request的header中必须含有Host头。例如"Host: localhost:8080"
* GET/POST的Response的header中必须含有Content-Length头。例如"Content-Length: 2"
* POST的Request的header中必须含有Content-Length头。例如"Content-Length: 1"。
* Header中只支持单值，格式为"Key: Value"，采用": "分割（注意冒号后有一个空格）。
* 一个HTTP的Request和Response分别都是一次HTTP报文传输，即不支持Chuck transfer encoding。

## HTTP Client
* 支持并发的HTTP请求发起和响应接收的处理。
* 使用连接池来保持keep-alive的连接，通过reuse提高性能。
* 对于一个host来说，client只能支持一定数量范围内的活跃长连接。

## HTTP Server
* 支持Request-URI与用户自定义的句柄的绑定，采用最长路径匹配的方式选取路径对应的句柄。
* 若没有对应匹配句柄，则匹配NotFoundHandler。
* 支持并发的HTTP的请求接收和响应发起的处理。

## 测试
以下测试会在服务端（包含tiny server和Go标准库的server)注册"/add"和"/value"句柄，句柄逻辑中访问名为value的int64类型的变量。"/add"为有副作用的POST请求，请求体中包含了delta，会进行value+=delta的操作，回应中不包含内容。"/value"为无副作用的GET请求，返回value的当前值，回应体中包含value字面量。

1. 使用Go标准客户端访问tiny server，发起GET和POST请求。验证tiny server对自定义句柄绑定和匹配，GET和POST的正确性。
2. 使用Go标准客户端访问tiny server，并发发起GET和POST请求。验证tiny server对并发访问请求的正常处理和响应时间是否低于阈值。
3. 使用tiny client访问Go标准服务端，发起GET和POST请求。验证client的GET和POST的正确性。
4. 使用tiny client访问Go标准服务端，并发发起GET和POST请求。验证tiny client对于并发访问请求的正常处理和响应时间是否低于阈值。
