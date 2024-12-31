# FastCGI Client

> [!IMPORTANT]
> This might not work out of the box for you. Read more below.

Though I eventually want to make this package usable in the near
future by adding the ability to configure things such as the 
server name and such, I have not done that yet.

This started out as an experiment in building my own tool
for local laravel development. I wanted to figure out
how exactly the setup we know (nginx and php-fpm) works
under the hood.

The [original spec](https://www.mit.edu/~yandros/doc/specs/fcgi-spec.html) for FastCGI
can be a little confusing to implement since it leaves out important details such
as how http headers are to be forwarded. Not to mention that if it's your first
time working with binary protocols (like it was for me), the explanations and code
examples can easily go over your head at first.

If you're interested in the inner workings of FastCGI, I'm providing you an easy
to understand implementation. The code is well commented and as explicit as
possible. No creative programming here.

## Code Tour 

If you don't want to read my explanation of the protocol and would rather look
through the code yourself first, here is where to start:

`main.go`: Has the code for connecting to php-fpm and returning a FastCGI client.

`client.go`: Everything related to writing and reading from the connection.

`request.go`: Everything related to packet/record structure and encoding.

If you start at the `Forward` function in `main.go`, you should be able
to follow the rest of the process from there. If you get confused, I'll
leave an explanation down below.

## The FastCGI protocol, explanation for humans

[Original FastCGI Spec](https://www.mit.edu/~yandros/doc/specs/fcgi-spec.html)

At first, the protocol might seem daunting and confusing, but it gets easier once
you understand that we're just forwarding a regular HTTP request over a binary
protocol to PHP FPM.

Binary protocol!? That sounds scary. It's not. The only difference from what you're
used to is that instead of sending text in a specific order, we're sending bytes
in a specific order.

### What we're sending to PHP-FPM

**Environment Variables**

There's a few important variables we need to send so PHP-FPM knows what
script to execute, how to execute it, and what the context of the request is.

- `SERVER_SOFTWARE` = myserver
- `QUERY_STRING` = ex: name=John&page=2 
- `REMOTE_ADDR` = server ip 
- `REQUEST_METHOD` = GET, POST, etc 
- `REQUEST_URI` = /the/path/requested 
- `SERVER_ADDR` = ip or domain name 
- `SERVER_PORT` = port you're serving on 
- `SERVER_NAME` = "localhost" or whatever makes sense to send here

**HTTP Headers**

The request headers also get send to PHP-FPM, however, they should
be send in the same way the environment variables above are sent.

Furthermore, the name needs to be changed to environment variable
casing. Ex: "Content-Type" => "CONTENT_TYPE".

Some variables are expected to be prefixed with "HTTP_". In my implementation
I just send both the variable without the HTTP_ prefix and with, since the
documentation for which variables need the prefix is non-existent.

**Request body**

If we have a POST request, we aso have to forward the body to PHP-FPM. This is
where the Content-Type header matters. The body could be:

- Url encoded form data
- Multipart form data (for file uploads)
- JSON

So the Content-Type header lets PHP know how to decode it. However, as far as
encoding the body for sending, it doesn't matter to us since it will follow
the same format regardless of content type. It's on PHP to figure out what
to do.

## The Actual Protocol

The FastCGI protocol is pretty simple. I'll be giving a simple explanation. For all the details
you can check out the original spec since it's an easier read once you get the basic idea.

### Records

"Records" are just network packets named differently. It's the way we send and receive data
in the FastCGI protocol.

Records have a type, which lets the FastCGI server know what kind of data is enclosed:

- `FCGI_BEGIN_REQUEST`
- `FCGI_ABORT_REQUEST`
- `FCGI_END_REQUEST`
- `FCGI_PARAMS`
- `FCGI_STDIN`
- `FCGI_STDOUT`

These are just names for integer constants. More on how these are used below.

### Streams vs single records

Certain records such as `FCGI_BEGIN_REQUEST` are sent as one record.

For longer content such as `FCGI_PARAMS` (for environment variables/headers), or `FCGI_STDIN`
(for the request body), are sent as a stream. A stream is a sequence of records of the
same type. To end the stream, we send an empty record (zero content length) of
the same type.

### Request Flow

**Terminology**

_Web server_: Your custom server, nginx, whatever is handling http

_fpm_: PHP-FPM

Record content will be denoted in the format:

`{Record type, content}`

Say we have an HTTP POST request with some data that we're forwarding to PHP-FPM.
The flow would look like this.

__Keep in mind this is not an exhaustive example. A lot more parameters will come from the browser_

**HTTP Request**

```
Content-Type: application/json
Content-Length: [whatever the length is]

{name: "Billie"}
```

**FastCGI**

```
Web server: {FCGI_BEGIN_REQUEST, ""}

Web server: {FCGI_PARAMS, "CONTENT_TYPE = application/json"}
Web server: {FCGI_PARAMS, "CONTENT_LENGTH = ??"}
Web server: {FCGI_PARAMS, ""} # End the stream

Web server: {FCGI_STDIN, {name: "Billie"}}
Web server: {FCGI_STDIN, ""} # End the stream

# We will read this response
fpm: {FCGI_STDOUT, [this will be the http response from PHP-FPM]}
fpm: {FCGI_STDOUT, ""} # End the stream

# After we're done reading the response, we turn it into
# an http response on our side and return it to the
# browser.
```
