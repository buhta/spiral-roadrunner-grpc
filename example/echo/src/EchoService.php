<?php
/**
 * Sample GRPC PHP server.
 */

use Service\EchoInterface;
use Service\Message;
use Spiral\RoadRunner\GRPC\ContextInterface;

class EchoService implements EchoInterface
{
    public function Ping(ContextInterface $ctx, Message $in): Message
    {
        throw \Spiral\RoadRunner\GRPC\Exception\InvokeException::create(
            'Internal',
            \Spiral\RoadRunner\GRPC\StatusCode::INTERNAL
        );
        $out = new Message();

        return $out->setMsg(date('Y-m-d H:i:s').': PONG');
    }
}
