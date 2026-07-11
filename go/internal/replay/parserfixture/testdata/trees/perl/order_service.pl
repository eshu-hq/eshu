#!/usr/bin/perl
use strict;
use warnings;

sub place_order {
    my ($repository, $customer_id, $skus) = @_;
    my $order = $repository->create($customer_id);
    for my $sku (@{$skus}) {
        add_line($order, $sku);
    }
    return $repository->save($order);
}

sub add_line {
    my ($order, $sku) = @_;
    push @{ $order->{lines} }, $sku;
    return $order;
}

sub find_order {
    my ($repository, $order_id) = @_;
    return $repository->by_id($order_id);
}

1;
